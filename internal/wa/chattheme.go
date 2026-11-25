package wa

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/photo"
)

// A chat's theme for everyone in it comes as a CHAT_THEME_SETTING protocol
// message (stanza type="media", mediatype="chat_theme"). It names a color
// scheme, a wallpaper or both, and what it doesn't name stays as it was, so
// what came is merged into one state per chat, kept in wz_meta
// (themeKey). The UI lays that over the look the user chose, and drops it
// when they choose another: whichever came last shows.
//
// A wallpaper that is a photo of the sender's (encrypted as "WhatsApp Chat
// Wallpaper Keys", mms type chat-wallpaper-image) or one of WhatsApp's own
// (by its number, from static.whatsapp.net) shows once it's downloaded, into
// <data>/chat-themes. Animated ones name a catalog this app doesn't have:
// they leave the wallpaper as it was.

const themeKey = "sharedtheme:"

// stockWallURL serves WhatsApp's own wallpapers (tests have their own).
var stockWallURL = "https://static.whatsapp.net/downloadable?category=wallpaper&id="

// stockWallID is what a stock wallpaper's ID looks like: "00014".
var stockWallID = regexp.MustCompile(`^[0-9A-Za-z_-]{1,32}$`)

// maxWallPixels bounds a shared photo: anyone in a group can set one, and a
// small file can hold a huge picture.
const maxWallPixels = 50_000_000

// themeState is a chat's shared theme as it's kept.
type themeState struct {
	// At is when it, or the look the user chose over it, was set, in unix
	// seconds of the server's clock: anything earlier comes too late.
	At     int64
	Reset  bool   `json:",omitempty"`
	Scheme string `json:",omitempty"`

	Wall        model.WallKind `json:",omitempty"`
	Doodles     bool           `json:",omitempty"`
	Light       uint32         `json:",omitempty"`
	Dark        uint32         `json:",omitempty"`
	DoodleLight uint32         `json:",omitempty"`
	DoodleDark  uint32         `json:",omitempty"`
	File        string         `json:",omitempty"` // in themeDir

	// Image (a ChatCustomImageWallpaper) or Stock is the photo to download,
	// which becomes the wallpaper once it's here.
	Image []byte `json:",omitempty"`
	Stock string `json:",omitempty"`
}

func (b *Backend) themeDir() string { return filepath.Join(b.dataDir, "chat-themes") }

func (b *Backend) themeState(ctx context.Context, chat string) (s themeState) {
	if raw := b.store.meta(ctx, themeKey+chat); raw != "" {
		_ = json.Unmarshal([]byte(raw), &s)
	}
	return s
}

// putTheme keeps a chat's state, dropping the photo the old one had.
func (b *Backend) putTheme(ctx context.Context, chat string, old, s themeState) {
	raw, _ := json.Marshal(s)
	if err := b.store.setMetaValue(ctx, themeKey+chat, string(raw)); err != nil {
		b.log.Warnf("store theme of %s: %v", chat, err)
		return
	}
	if old.File != "" && old.File != s.File {
		_ = os.Remove(filepath.Join(b.themeDir(), filepath.Base(old.File)))
	}
}

// SharedTheme implements model.Backend.
func (b *Backend) SharedTheme(chatID string) *model.SharedTheme {
	s := b.themeState(b.ctx, chatID)
	if !s.Reset && s.Scheme == "" && s.Wall == model.WallKeep {
		return nil
	}
	t := &model.SharedTheme{Reset: s.Reset, Scheme: s.Scheme, Wall: model.SharedWall{Kind: s.Wall, Doodles: s.Doodles,
		Light: s.Light, Dark: s.Dark, DoodleLight: s.DoodleLight, DoodleDark: s.DoodleDark}}
	if s.Wall == model.WallPhoto {
		t.Wall.Photo = filepath.Join(b.themeDir(), filepath.Base(s.File))
	}
	return t
}

// DropSharedTheme implements model.Backend.
func (b *Backend) DropSharedTheme(chatID string) {
	b.themeMu.Lock()
	defer b.themeMu.Unlock()
	b.putTheme(b.ctx, chatID, b.themeState(b.ctx, chatID), themeState{At: b.sendTime().Unix()})
}

// noteTheme takes the theme a message sets for everyone in chat.
func (b *Backend) noteTheme(ctx context.Context, chat string, e *events.Message) {
	pm := unwrap(e.Message).GetProtocolMessage()
	if pm.GetType() != waE2E.ProtocolMessage_CHAT_THEME_SETTING || pm.GetChatThemeSetting() == nil || !themeChat(e.Info.Chat) {
		return
	}
	// The stanza's time is the server's; the setting's own is the sender's
	// word, and one far ahead would keep every later theme out.
	if b.mergeTheme(ctx, chat, pm.GetChatThemeSetting(), e.Info.Timestamp.Unix()) {
		b.emit(model.ChatThemeEvent{ChatID: chat})
		b.goWork(func() { b.fetchTheme(ctx, chat) })
	}
}

// themeChat reports whether a chat's theme can be set for everyone.
func themeChat(j types.JID) bool {
	return j.Server == types.GroupServer || j.Server == types.DefaultUserServer || j.Server == types.HiddenUserServer
}

// mergeTheme merges a theme set at at into a chat's, unless what it has was
// set later.
func (b *Backend) mergeTheme(ctx context.Context, chat string, t *waE2E.ChatThemeSetting, at int64) bool {
	b.themeMu.Lock()
	defer b.themeMu.Unlock()
	old := b.themeState(ctx, chat)
	if at < old.At {
		return false
	}
	s := old
	if t.GetClearTheme() {
		s = themeState{Reset: true}
	}
	s.At = at
	if t.GetColorSchemeID() != "" {
		s.Scheme = t.GetColorSchemeID()
	}
	// wall starts a new wallpaper, without anything of the last one's.
	wall := func(kind model.WallKind) {
		s = themeState{At: s.At, Reset: s.Reset, Scheme: s.Scheme, Wall: kind}
	}
	switch w := t.GetWallpaper().(type) {
	case *waE2E.ChatThemeSetting_DefaultWallpaper:
		wall(model.WallDefault)
		s.Doodles = w.DefaultWallpaper.GetIsDoodleEnabled()
	case *waE2E.ChatThemeSetting_SolidColor:
		c := w.SolidColor
		if c.BackgroundLightArgb == nil && c.BackgroundDarkArgb == nil {
			break
		}
		wall(model.WallColor)
		s.Doodles = c.GetIsDoodleEnabled()
		s.Light, s.Dark = c.GetBackgroundLightArgb()&0xffffff, c.GetBackgroundDarkArgb()&0xffffff
		if c.BackgroundLightArgb == nil {
			s.Light = s.Dark
		} else if c.BackgroundDarkArgb == nil {
			s.Dark = s.Light
		}
		s.DoodleLight, s.DoodleDark = c.GetDoodleLightArgb(), c.GetDoodleDarkArgb()
	case *waE2E.ChatThemeSetting_StockImage:
		if id := w.StockImage.GetStockImageID(); stockWallID.MatchString(id) {
			s.Image, s.Stock = nil, id
		}
	case *waE2E.ChatThemeSetting_CustomImage:
		s.Image, s.Stock = marshal(w.CustomImage), ""
	case *waE2E.ChatThemeSetting_AnimatedWallpaper:
		b.log.Infof("animated wallpaper %q of %s: not shown", w.AnimatedWallpaper.GetAnimatedWallpaperID(), chat)
	}
	b.putTheme(ctx, chat, old, s)
	return true
}

// fetchThemes downloads the wallpapers that didn't make it before.
func (b *Backend) fetchThemes(ctx context.Context) {
	rows, err := b.db.QueryContext(ctx, `SELECT key FROM wz_meta WHERE key LIKE ? || '%' AND
		(value LIKE '%"Image":%' OR value LIKE '%"Stock":%')`, themeKey)
	if err != nil {
		return
	}
	var chats []string
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			chats = append(chats, k[len(themeKey):])
		}
	}
	rows.Close()
	for _, chat := range chats {
		b.fetchTheme(ctx, chat)
	}
}

// fetchTheme downloads the photo a chat's shared theme waits for, if any.
// One that can't be had any more is forgotten; after another failure the
// next connection tries again.
func (b *Backend) fetchTheme(ctx context.Context, chat string) {
	want := b.themeState(ctx, chat)
	var data []byte
	var err error
	switch {
	case want.Stock != "":
		data, err = httpGet(ctx, stockWallURL+url.QueryEscape("wallpaper-"+want.Stock+"_web_2400"))
	case len(want.Image) > 0:
		data, err = b.fetchThemeImage(ctx, want.Image)
	default:
		return
	}
	file := ""
	if err == nil {
		if file, err = b.keepThemePhoto(chat, data); err != nil {
			err = fmt.Errorf("%w: %v", errBadWallpaper, err)
		}
	}
	gone := errors.Is(err, errBadWallpaper) || errors.Is(err, whatsmeow.ErrInvalidMediaSHA256) ||
		errors.Is(err, whatsmeow.ErrInvalidMediaEncSHA256) || errors.Is(err, whatsmeow.ErrInvalidMediaHMAC) ||
		errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith403) ||
		errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith404) || errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith410)
	if err != nil {
		b.log.Warnf("wallpaper of %s: %v", chat, err)
		if !gone {
			return
		}
	}

	b.themeMu.Lock()
	defer b.themeMu.Unlock()
	old := b.themeState(ctx, chat)
	if old.Stock != want.Stock || !bytes.Equal(old.Image, want.Image) {
		// Another theme came meanwhile, or the user chose a look.
		if file != "" && file != old.File {
			_ = os.Remove(filepath.Join(b.themeDir(), file))
		}
		return
	}
	s := old
	s.Image, s.Stock = nil, ""
	if err == nil {
		s = themeState{At: s.At, Reset: s.Reset, Scheme: s.Scheme, Wall: model.WallPhoto, File: file}
	}
	b.putTheme(ctx, chat, old, s)
	if err == nil {
		b.emit(model.ChatThemeEvent{ChatID: chat})
	}
}

var errBadWallpaper = errors.New("unusable wallpaper")

// fetchThemeImage downloads a photo of the sender's.
func (b *Backend) fetchThemeImage(ctx context.Context, raw []byte) ([]byte, error) {
	var w waE2E.ChatCustomImageWallpaper
	if err := proto.Unmarshal(raw, &w); err != nil || len(w.GetMediaKey()) != 32 || len(w.GetFileSHA256()) != 32 ||
		len(w.GetFileEncSHA256()) != 32 || w.GetDirectPath() == "" {
		return nil, errBadWallpaper
	}
	cli := b.client()
	if cli == nil || !cli.IsConnected() {
		return nil, errors.New("offline")
	}
	return cli.DownloadMediaWithPath(ctx, w.GetDirectPath(), w.GetFileEncSHA256(), w.GetFileSHA256(), w.GetMediaKey(),
		whatsmeow.MediaChatWallpaper, "", false)
}

// keepThemePhoto checks that data is a photo the app can show and keeps it
// for a chat, returning its name in themeDir.
func (b *Backend) keepThemePhoto(chat string, data []byte) (string, error) {
	c, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	if c.Width <= 0 || c.Height <= 0 || c.Width > maxWallPixels/c.Height {
		return "", fmt.Errorf("%dx%d pixels", c.Width, c.Height)
	}
	// Named by the chat too, so a chat's can go without another's.
	h := sha256.New()
	h.Write([]byte(chat + "\x00"))
	h.Write(data)
	name := hex.EncodeToString(h.Sum(nil)[:12]) + "." + format
	if err := os.MkdirAll(b.themeDir(), 0o700); err != nil {
		return "", err
	}
	return name, os.WriteFile(filepath.Join(b.themeDir(), name), data, 0o600)
}

// themeSystem is the note a theme leaves in its chat.
func themeSystem(evt *events.Message, chat, actor types.JID, t *waE2E.ChatThemeSetting) (storedMsg, bool) {
	if evt.Info.ID == "" || t == nil || !themeChat(evt.Info.Chat) {
		return storedMsg{}, false
	}
	return themeNote(chat.String(), evt.Info.ID, evt.Info.Timestamp, actor, evt.Info.IsFromMe, t.GetClearTheme()), true
}

func themeNote(chat, id string, at time.Time, actor types.JID, fromMe, reset bool) storedMsg {
	var params []string
	if reset {
		params = []string{"clear"}
	}
	return systemMsg(chat, id, at, actor, fromMe, stubChatTheme, params...)
}

// shareSide is the long side a photo is sent at: a tall phone's screen.
const shareSide = 2560

// ShareTheme implements model.Backend.
func (b *Backend) ShareTheme(chatID string, t model.SharedTheme) {
	jid, err := types.ParseJID(chatID)
	if err != nil || !themeChat(jid) {
		return
	}
	cli := b.connected()
	if cli == nil {
		return
	}
	b.goWork(func() {
		if err := b.shareTheme(b.ctx, cli, jid, t); err != nil {
			b.log.Errorf("share theme in %s: %v", chatID, err)
			b.emit(model.NoticeEvent{Text: "Couldn't set the theme for everyone."})
			return
		}
		text := "Theme set for everyone"
		if t.Reset {
			text = "Theme reset for everyone"
		}
		b.emit(model.NoticeEvent{Text: text})
	})
}

func (b *Backend) shareTheme(ctx context.Context, cli *whatsmeow.Client, jid types.JID, t model.SharedTheme) error {
	set, err := themeSetting(t, b.sendTime())
	if err != nil {
		return err
	}
	if t.Wall.Kind == model.WallPhoto {
		data, err := sharedPhoto(t.Wall.Photo, t.Wall.Crop)
		if err != nil {
			return err
		}
		up, err := cli.Upload(ctx, data, whatsmeow.MediaChatWallpaper)
		if err != nil {
			return err
		}
		set.Wallpaper = &waE2E.ChatThemeSetting_CustomImage{CustomImage: &waE2E.ChatCustomImageWallpaper{
			DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256,
		}}
	}
	id := cli.GenerateMessageID()
	msg := &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_CHAT_THEME_SETTING.Enum(), ChatThemeSetting: set,
	}}
	if _, err := b.sendMessage(ctx, cli, jid, msg, whatsmeow.SendRequestExtra{ID: id}); err != nil {
		return err
	}
	b.putSystem(ctx, themeNote(jid.String(), id, b.sendTime(), b.ownJID(jid.String()), true, t.Reset))
	return nil
}

// themeSetting is t as it's sent, but for a photo, which is uploaded first.
func themeSetting(t model.SharedTheme, now time.Time) (*waE2E.ChatThemeSetting, error) {
	set := &waE2E.ChatThemeSetting{SettingTimestampMS: proto.Int64(now.UnixMilli())}
	if t.Reset {
		set.ClearTheme = proto.Bool(true)
		return set, nil
	}
	if t.Scheme != "" {
		set.ColorSchemeID = proto.String(t.Scheme)
	}
	const opaque = 0xff000000
	switch w := t.Wall; w.Kind {
	case model.WallDefault:
		set.Wallpaper = &waE2E.ChatThemeSetting_DefaultWallpaper{DefaultWallpaper: &waE2E.ChatDefaultWallpaper{
			IsDoodleEnabled: proto.Bool(w.Doodles)}}
	case model.WallColor:
		c := &waE2E.ChatSolidColorWallpaper{BackgroundLightArgb: proto.Uint32(opaque | w.Light),
			BackgroundDarkArgb: proto.Uint32(opaque | w.Dark), IsDoodleEnabled: proto.Bool(w.Doodles)}
		if w.DoodleLight != 0 {
			c.DoodleLightArgb = proto.Uint32(w.DoodleLight)
		}
		if w.DoodleDark != 0 {
			c.DoodleDarkArgb = proto.Uint32(w.DoodleDark)
		}
		set.Wallpaper = &waE2E.ChatThemeSetting_SolidColor{SolidColor: c}
	case model.WallKeep:
		if t.Scheme == "" {
			return nil, errors.New("nothing to set")
		}
	}
	return set, nil
}

// sharedPhoto is the part crop of the photo at path, or all of it when
// that's empty, as it's sent: always encoded again, so nothing of the
// file's but the picture (where it was taken) goes.
func sharedPhoto(path string, crop image.Rectangle) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if !crop.Empty() {
		sub, ok := img.(interface {
			SubImage(image.Rectangle) image.Image
		})
		crop = crop.Add(img.Bounds().Min).Intersect(img.Bounds())
		if !ok || crop.Empty() {
			return nil, fmt.Errorf("can't crop a %T of %v to %v", img, img.Bounds(), crop)
		}
		img = sub.SubImage(crop)
	}
	out, _, _, err := photo.Fit(img, shareSide)
	return out, err
}

// logChatTheme writes a theme to the log with -debug, whole: the log's
// stanza holds only its encrypted size. The hex has what the JSON leaves
// out, fields the protobuf doesn't know. A photo's media key is in both.
func (b *Backend) logChatTheme(e *events.Message) {
	pm := unwrap(e.Message).GetProtocolMessage()
	if e.Info.MediaType != "chat_theme" && pm.GetType() != waE2E.ProtocolMessage_CHAT_THEME_SETTING {
		return
	}
	m := e.RawMessage
	if m == nil {
		m = e.Message
	}
	raw, err := proto.Marshal(m)
	if err != nil {
		b.log.Debugf("chat theme %s: %v", e.Info.ID, err)
		return
	}
	js, _ := protojson.Marshal(m)
	b.log.Debugf("chat theme %s in %s from %s: %s raw %x", e.Info.ID, e.Info.Chat, e.Info.Sender, js, raw)
}
