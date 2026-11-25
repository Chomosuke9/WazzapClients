package wa

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

const themeGroup = "120363000000000001@g.us"

func themeEvent(id string, at int64, t *waE2E.ChatThemeSetting) *events.Message {
	return &events.Message{
		Info: types.MessageInfo{ID: id, Timestamp: time.Unix(at, 0), Type: "media", MediaType: "chat_theme",
			MessageSource: types.MessageSource{Chat: types.NewJID("120363000000000001", types.GroupServer),
				Sender: types.NewJID("200000000000001", types.HiddenUserServer), IsGroup: true}},
		Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_CHAT_THEME_SETTING.Enum(), ChatThemeSetting: t}},
	}
}

func scheme(id string) *waE2E.ChatThemeSetting {
	return &waE2E.ChatThemeSetting{ColorSchemeID: proto.String(id)}
}

func solid(argb uint32) *waE2E.ChatThemeSetting {
	return &waE2E.ChatThemeSetting{Wallpaper: &waE2E.ChatThemeSetting_SolidColor{SolidColor: &waE2E.ChatSolidColorWallpaper{
		BackgroundLightArgb: proto.Uint32(argb), BackgroundDarkArgb: proto.Uint32(argb), IsDoodleEnabled: proto.Bool(false)}}}
}

// A theme set for everyone is merged into the chat's, noted in the chat,
// and forgotten once the user picks a look; what was set before that and
// comes late changes nothing.
func TestSharedTheme(t *testing.T) {
	b := testBackend(t)
	now := time.Unix(2000, 0)
	b.clock = func() time.Time { return now }
	if b.SharedTheme(themeGroup) != nil {
		t.Fatal("a chat has a shared theme before one comes")
	}

	b.onMessage(themeEvent("A", 1000, solid(0xFFFF0000)))
	b.onMessage(themeEvent("B", 1010, scheme("Default-Blue@Minimal")))
	got := b.SharedTheme(themeGroup)
	if got == nil || got.Scheme != "Default-Blue@Minimal" || got.Wall.Kind != model.WallColor ||
		got.Wall.Light != 0xff0000 || got.Wall.Dark != 0xff0000 || got.Wall.Doodles {
		t.Fatalf("a color then a scheme gave %+v", got)
	}
	var events int
	for _, e := range b.Poll() {
		if e, ok := e.(model.ChatThemeEvent); ok && e.ChatID == themeGroup {
			events++
		}
	}
	if events != 2 {
		t.Errorf("%d ChatThemeEvents, want 2", events)
	}
	msgs := b.Messages(themeGroup, 10)
	if len(msgs) != 2 || msgs[0].Kind != model.KindSystem || !strings.HasSuffix(msgs[0].Text, "changed the chat theme") {
		t.Fatalf("the chat's notes are %+v", msgs)
	}

	// One set earlier, as history sync brings it.
	b.onMessage(themeEvent("OLD", 900, scheme("Pink")))
	if got := b.SharedTheme(themeGroup); got.Scheme != "Default-Blue@Minimal" {
		t.Errorf("an older theme replaced a newer: %+v", got)
	}

	// The user's own choice, at 2000; then one set before it comes.
	b.DropSharedTheme(themeGroup)
	if got := b.SharedTheme(themeGroup); got != nil {
		t.Errorf("a dropped theme is still there: %+v", got)
	}
	b.onMessage(themeEvent("LATE", 1990, scheme("Pink")))
	if got := b.SharedTheme(themeGroup); got != nil {
		t.Errorf("a theme from before the user's choice came over it: %+v", got)
	}
	b.onMessage(themeEvent("NEXT", 2010, scheme("Pink")))
	if got := b.SharedTheme(themeGroup); got == nil || got.Scheme != "Pink" || got.Wall.Kind != model.WallKeep {
		t.Errorf("the next theme gave %+v", got)
	}

	// Clearing drops it all.
	b.onMessage(themeEvent("CLEAR", 2020, &waE2E.ChatThemeSetting{ClearTheme: proto.Bool(true)}))
	if got := b.SharedTheme(themeGroup); got == nil || *got != (model.SharedTheme{Reset: true}) {
		t.Errorf("a cleared theme is %+v", got)
	}
	msgs = b.Messages(themeGroup, 1)
	if len(msgs) != 1 || !strings.HasSuffix(msgs[0].Text, "reset the chat theme") {
		t.Errorf("the clearing's note is %+v", msgs)
	}
}

// The sender's clock doesn't decide which theme is the latest, or one set
// far ahead would keep every later theme out.
func TestSharedThemeClock(t *testing.T) {
	b := testBackend(t)
	ahead := scheme("Red")
	ahead.SettingTimestampMS = proto.Int64(99999999999999)
	b.onMessage(themeEvent("A", 1000, ahead))
	b.onMessage(themeEvent("B", 1001, scheme("Pink")))
	if got := b.SharedTheme(themeGroup); got == nil || got.Scheme != "Pink" {
		t.Errorf("the theme is %+v", got)
	}
}

// A wallpaper of WhatsApp's shows once it's downloaded, the last one until
// then; one that isn't a picture, or a huge one, never does.
func TestSharedStockWallpaper(t *testing.T) {
	var pic bytes.Buffer
	if err := jpeg.Encode(&pic, image.NewRGBA(image.Rect(0, 0, 20, 30)), nil); err != nil {
		t.Fatal(err)
	}
	body := pic.Bytes()
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Query().Get("id")
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	old := stockWallURL
	stockWallURL = srv.URL + "/?id="
	defer func() { stockWallURL = old }()
	stock := func(id string) *waE2E.ChatThemeSetting {
		return &waE2E.ChatThemeSetting{Wallpaper: &waE2E.ChatThemeSetting_StockImage{
			StockImage: &waE2E.ChatStockImageWallpaper{StockImageID: proto.String(id)}}}
	}

	b := testBackend(t)
	b.onMessage(themeEvent("A", 1000, solid(0xFF00FF00)))
	b.onMessage(themeEvent("B", 1001, stock("00014")))
	b.work.Wait()
	got := b.SharedTheme(themeGroup)
	if asked != "wallpaper-00014_web_2400" || got == nil || got.Wall.Kind != model.WallPhoto {
		t.Fatalf("asked for %q, got %+v", asked, got)
	}
	if data, err := os.ReadFile(got.Wall.Photo); err != nil || !bytes.Equal(data, body) {
		t.Errorf("its file: %d bytes, %v", len(data), err)
	}

	// Something else, then an ID that isn't one.
	first := got.Wall.Photo
	body = []byte("<html>not a picture</html>")
	b.onMessage(themeEvent("C", 1002, stock("00002")))
	b.onMessage(themeEvent("D", 1003, stock("../../etc")))
	b.work.Wait()
	if got := b.SharedTheme(themeGroup); got.Wall.Photo != first {
		t.Errorf("the wallpaper became %+v", got.Wall)
	}
	if s := b.themeState(b.ctx, themeGroup); s.Stock != "" {
		t.Errorf("%q is still to be downloaded", s.Stock)
	}

	// Its file goes with it.
	b.DropSharedTheme(themeGroup)
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Errorf("the dropped wallpaper's file is still there (%v)", err)
	}
}

// A photo of the sender's waits for the connection, across restarts.
func TestSharedPhotoWaits(t *testing.T) {
	b := testBackend(t)
	b.onMessage(themeEvent("A", 1000, &waE2E.ChatThemeSetting{ColorSchemeID: proto.String("Pink"),
		Wallpaper: &waE2E.ChatThemeSetting_CustomImage{CustomImage: &waE2E.ChatCustomImageWallpaper{
			DirectPath: proto.String("/v/x"), MediaKey: make([]byte, 32), FileSHA256: make([]byte, 32), FileEncSHA256: make([]byte, 32)}}}))
	b.work.Wait()
	if got := b.SharedTheme(themeGroup); got == nil || got.Scheme != "Pink" || got.Wall.Kind != model.WallKeep {
		t.Errorf("offline it's %+v", got)
	}
	if s := b.themeState(b.ctx, themeGroup); len(s.Image) == 0 {
		t.Error("the photo was forgotten while offline")
	}
	// Without its keys it can't ever come.
	b.onMessage(themeEvent("B", 1001, &waE2E.ChatThemeSetting{Wallpaper: &waE2E.ChatThemeSetting_CustomImage{
		CustomImage: &waE2E.ChatCustomImageWallpaper{DirectPath: proto.String("/v/x")}}}))
	b.work.Wait()
	if s := b.themeState(b.ctx, themeGroup); len(s.Image) != 0 {
		t.Error("a photo without keys is still to be downloaded")
	}
}

// A picture of more pixels than any wallpaper is refused, whatever its
// file's size.
func TestKeepThemePhoto(t *testing.T) {
	b := testBackend(t)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, 9000, 6000)), &jpeg.Options{Quality: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.keepThemePhoto(themeGroup, buf.Bytes()); err == nil {
		t.Error("a 54 megapixel picture was kept")
	}
}

// A photo is sent cropped to the part that has the point picked in its middle.
func TestSharedPhotoCrop(t *testing.T) {
	// A red photo, blue where the crop is.
	img := image.NewRGBA(image.Rect(0, 0, 200, 400))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)
	crop := image.Rect(40, 0, 200, 200)
	draw.Draw(img, crop, image.NewUniform(color.RGBA{B: 255, A: 255}), image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "p.png")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		crop image.Rectangle
		size image.Point
	}{{image.Rectangle{}, image.Pt(200, 400)}, {crop, image.Pt(160, 200)}} {
		data, err := sharedPhoto(path, c.crop)
		if err != nil {
			t.Fatal(err)
		}
		got, _, err := image.Decode(bytes.NewReader(data))
		if err != nil || got.Bounds().Size() != c.size {
			t.Fatalf("crop %v: sent %v, %v, want %v", c.crop, got.Bounds(), err, c.size)
		}
		if r, _, b, _ := got.At(4, 4).RGBA(); (b > r) == c.crop.Empty() {
			t.Errorf("crop %v: the corner is red %d blue %d", c.crop, r>>8, b>>8)
		}
	}
	if _, err := sharedPhoto(path, image.Rect(900, 900, 950, 950)); err == nil {
		t.Error("a crop outside the photo was sent")
	}
}

// What a theme is sent as.
func TestThemeSetting(t *testing.T) {
	now := time.UnixMilli(1795606752000)
	for _, c := range []struct {
		name string
		t    model.SharedTheme
		want *waE2E.ChatThemeSetting
	}{
		{"reset", model.SharedTheme{Reset: true, Scheme: "Pink"}, &waE2E.ChatThemeSetting{ClearTheme: proto.Bool(true)}},
		{"bubbles", model.SharedTheme{Scheme: "Pink"}, scheme("Pink")},
		{"color", model.SharedTheme{Wall: model.SharedWall{Kind: model.WallColor, Light: 0xff0000, Dark: 0xff0000}}, solid(0xFFFF0000)},
		{"theme", model.SharedTheme{Scheme: "Pink", Wall: model.SharedWall{Kind: model.WallDefault, Doodles: true}},
			&waE2E.ChatThemeSetting{ColorSchemeID: proto.String("Pink"), Wallpaper: &waE2E.ChatThemeSetting_DefaultWallpaper{
				DefaultWallpaper: &waE2E.ChatDefaultWallpaper{IsDoodleEnabled: proto.Bool(true)}}}},
	} {
		c.want.SettingTimestampMS = proto.Int64(now.UnixMilli())
		if got, err := themeSetting(c.t, now); err != nil || !proto.Equal(got, c.want) {
			t.Errorf("%s: %v, %v", c.name, got, err)
		}
	}
	if _, err := themeSetting(model.SharedTheme{}, now); err == nil {
		t.Error("an empty theme was sent")
	}
}

// A theme change is logged with -debug only, the fields the protobuf doesn't
// know included; other protocol messages, which hold keys, never are.
func TestLogChatTheme(t *testing.T) {
	unknown := &waE2E.Message{}
	if err := proto.Unmarshal([]byte{0xca, 0x3e, 0x02, 'h', 'i'}, unknown); err != nil {
		t.Fatal(err)
	}
	theme := themeEvent("T", 1, scheme("blue")).Message
	keys := &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_APP_STATE_SYNC_KEY_SHARE.Enum()}}
	for _, c := range []struct {
		name, mediaType, want string
		msg                   *waE2E.Message
		debug                 bool
	}{
		{"known", "", `"blue"`, theme, true},
		{"unknown fields", "chat_theme", "rawca3e026869", unknown, true},
		{"without -debug", "chat_theme", "", theme, false},
		{"another protocol message", "", "", keys, true},
	} {
		var out bytes.Buffer
		b := testBackend(t)
		b.log = newLogger(&out, c.debug)
		e := snippetEvent("T1", c.msg)
		e.Info.MediaType = c.mediaType
		b.onMessage(e)
		got := strings.ReplaceAll(out.String(), " ", "")
		if c.want == "" && strings.Contains(got, "chattheme") || !strings.Contains(got, c.want) {
			t.Errorf("%s: logged %q, want %q", c.name, out.String(), c.want)
		}
	}
}
