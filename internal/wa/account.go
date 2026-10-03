package wa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // profile photos may be PNG
	"os"
	"strconv"
	"time"

	_ "golang.org/x/image/webp" // or WebP

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/appstate"
	waBinary "github.com/polymorfa/hypermeow/binary"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/photo"
)

// Meta keys of the account details. accountKey caches what the server
// returned last (privacy, about, blocked contacts), so Settings shows
// them offline too.
const (
	accountKey      = "account"
	linkedKey       = "linked_at"     // when this device was paired (RFC 3339)
	defaultTimerKey = "default_timer" // seconds; set by history sync and SetDefaultTimer
	// prefSecurityNotify mirrors the UI's pref of the same name: show a
	// notice when a contact's security code changes. Off unless "on".
	prefSecurityNotify = "security_notifications"
)

// profilePhotoSize is the side of the square picture WhatsApp keeps.
const profilePhotoSize = 640

// Account implements model.Backend.
func (b *Backend) Account() *model.Account {
	ctx := b.ctx
	a := new(model.Account)
	if raw := b.store.meta(ctx, accountKey); raw != "" {
		_ = json.Unmarshal([]byte(raw), a)
	}
	if t, err := time.Parse(time.RFC3339, b.store.meta(ctx, linkedKey)); err == nil {
		a.Linked = t
	}
	if v := b.store.meta(ctx, defaultTimerKey); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			a.DefaultTimer, a.TimerKnown = time.Duration(n)*time.Second, true
		}
	}
	if cli := b.client(); cli != nil {
		if cli.Store.ID != nil {
			id := cli.Store.ID.ToNonAD()
			a.ID, a.Phone = id.String(), formatPhone(id.User)
		}
		if !cli.Store.LID.IsEmpty() {
			a.LID = cli.Store.LID.ToNonAD().String()
		}
		if cli.Store.PushName != "" {
			a.Name = cli.Store.PushName
		}
		if !b.accountFetched.Swap(true) {
			go b.refreshAccount()
		}
	}
	return a
}

// saveAccount applies change to the cached account details and announces
// the result.
func (b *Backend) saveAccount(change func(a *model.Account)) {
	b.accountMu.Lock()
	defer b.accountMu.Unlock()
	ctx := b.ctx
	var a model.Account
	if raw := b.store.meta(ctx, accountKey); raw != "" {
		_ = json.Unmarshal([]byte(raw), &a)
	}
	change(&a)
	out, _ := json.Marshal(&a)
	_ = b.store.setMetaValue(ctx, accountKey, string(out))
	b.emit(model.AccountEvent{})
}

// refreshAccount fetches your about text, privacy settings and blocked
// contacts.
func (b *Backend) refreshAccount() {
	cli := b.client()
	if cli == nil || !cli.IsConnected() || cli.Store.ID == nil {
		b.accountFetched.Store(false) // try again when asked next
		return
	}
	ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
	defer cancel()
	me := cli.Store.ID.ToNonAD()
	if info, err := cli.GetUserInfo(ctx, []types.JID{me}); err != nil {
		b.log.Debugf("own user info: %v", err)
	} else if u, ok := info[me]; ok {
		b.saveAccount(func(a *model.Account) { a.About, a.Username = u.Status, u.Username })
	}
	if ps, err := cli.TryFetchPrivacySettings(ctx, true); err != nil {
		b.log.Debugf("privacy settings: %v", err)
	} else {
		b.saveAccount(func(a *model.Account) { a.Privacy = privacyMap(ps) })
	}
	b.refreshBlocklist(ctx)
}

func (b *Backend) refreshBlocklist(ctx context.Context) {
	cli := b.client()
	if cli == nil {
		return
	}
	list, err := cli.GetBlocklist(ctx)
	if err != nil {
		b.log.Debugf("blocklist: %v", err)
		return
	}
	b.setBlocklist(ctx, list)
}

func (b *Backend) setBlocklist(ctx context.Context, list *types.Blocklist) {
	if list == nil {
		return
	}
	blocked := make([]model.Contact, 0, len(list.JIDs))
	for _, j := range list.JIDs {
		j = b.canonical(ctx, j)
		blocked = append(blocked, model.Contact{ID: j.String(), Name: b.chatName(ctx, j)})
	}
	b.saveAccount(func(a *model.Account) { a.Blocked, a.BlockedKnown = blocked, true })
}

func privacyMap(ps *types.PrivacySettings) map[string]string {
	m := map[string]string{}
	set := func(k string, v types.PrivacySetting) {
		if v != types.PrivacySettingUndefined {
			m[k] = string(v)
		}
	}
	set(model.PrivacyLastSeen, ps.LastSeen)
	set(model.PrivacyOnline, ps.Online)
	set(model.PrivacyPhoto, ps.Profile)
	set(model.PrivacyAbout, ps.Status)
	set(model.PrivacyGroups, ps.GroupAdd)
	set(model.PrivacyReadReceipts, ps.ReadReceipts)
	return m
}

// onAccountEvent handles the hypermeow events that change account details.
func (b *Backend) onAccountEvent(evt any) {
	ctx := b.ctx
	switch e := evt.(type) {
	case *events.PairSuccess:
		_ = b.store.setMetaValue(ctx, linkedKey, time.Now().Format(time.RFC3339))
	case *events.PrivacySettings:
		ps := e.NewSettings
		b.saveAccount(func(a *model.Account) { a.Privacy = privacyMap(&ps) })
	case *events.Blocklist:
		go func() {
			ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
			defer cancel()
			b.refreshBlocklist(ctx)
		}()
	case *events.PushNameSetting:
		b.emit(model.AccountEvent{})
	case *events.Picture:
		b.onPicture(e)
	case *events.IdentityChange:
		if b.Pref(prefSecurityNotify) != "on" || e.JID.Server == types.GroupServer {
			return
		}
		name := b.chatName(ctx, b.canonical(ctx, e.JID))
		b.emit(model.NoticeEvent{Text: "Your security code with " + name + " changed."})
	}
}

// SetProfileName implements model.Backend.
func (b *Backend) SetProfileName(name string) {
	cli := b.connected()
	if cli == nil || name == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
		defer cancel()
		if err := cli.SendAppState(ctx, appstate.BuildSettingPushName(name)); err != nil {
			b.log.Warnf("set push name: %v", err)
			b.emit(model.NoticeEvent{Text: "Couldn't change your name."})
			return
		}
		cli.Store.PushName = name
		if err := cli.Store.Save(ctx); err != nil {
			b.log.Warnf("save push name: %v", err)
		}
		b.names.clear()
		b.emit(model.AccountEvent{})
	}()
}

// SetAbout implements model.Backend.
func (b *Backend) SetAbout(about string) {
	cli := b.connected()
	if cli == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
		defer cancel()
		// The GraphQL mutation is refused for some accounts (400 Bad
		// Request); the older status IQ still works for them.
		if err := cli.SetStatusMessage(ctx, types.SetStatusInput{Text: &about}); err != nil {
			b.log.Debugf("set about (mex): %v", err)
			if _, err := cli.DangerousInternals().SendIQ(ctx, whatsmeow.DangerousInfoQuery{
				Namespace: "status",
				Type:      "set",
				To:        types.ServerJID,
				Content:   []waBinary.Node{{Tag: "status", Content: []byte(about)}},
			}); err != nil {
				b.log.Warnf("set about: %v", err)
				b.emit(model.NoticeEvent{Text: "Couldn't change your about."})
				return
			}
		}
		b.saveAccount(func(a *model.Account) { a.About = about })
	}()
}

// SetProfilePhoto implements model.Backend. The picture is cropped to a
// centered square and scaled down to profilePhotoSize.
func (b *Backend) SetProfilePhoto(path string) {
	cli := b.connected()
	if cli == nil || cli.Store.ID == nil {
		return
	}
	me := cli.Store.ID.ToNonAD().String()
	go func() {
		var data []byte
		if path != "" {
			var err error
			if data, err = profilePhoto(path); err != nil {
				b.log.Warnf("profile photo %s: %v", path, err)
				b.emit(model.NoticeEvent{Text: "That picture can't be used."})
				return
			}
		}
		ctx, cancel := context.WithTimeout(b.ctx, 60*time.Second)
		defer cancel()
		// Your own picture is set without a target.
		if _, err := cli.SetGroupPhoto(ctx, types.EmptyJID, data); err != nil {
			b.log.Warnf("set profile photo: %v", err)
			b.emit(model.NoticeEvent{Text: "Couldn't change your profile photo."})
			return
		}
		b.fetchAvatar(me)
		if path == "" {
			b.emit(model.NoticeEvent{Text: "Profile photo removed"})
		} else {
			b.emit(model.NoticeEvent{Text: "Profile photo updated"})
		}
	}()
}

// profilePhoto reads a picture and returns it as a square JPEG.
func profilePhoto(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if cfg, _, err := image.DecodeConfig(f); err != nil {
		return nil, err
	} else if cfg.Width*cfg.Height > 50_000_000 {
		return nil, fmt.Errorf("picture too large: %dx%d", cfg.Width, cfg.Height)
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	r := img.Bounds()
	side := min(r.Dx(), r.Dy())
	sq := image.Rectangle{Min: image.Pt(r.Min.X+(r.Dx()-side)/2, r.Min.Y+(r.Dy()-side)/2)}
	sq.Max = sq.Min.Add(image.Pt(side, side))
	sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return nil, fmt.Errorf("can't crop %T", img)
	}
	out := min(side, profilePhotoSize)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, photo.Shrink(sub.SubImage(sq), out, out), &jpeg.Options{Quality: 90}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// SetPrivacy implements model.Backend.
func (b *Backend) SetPrivacy(key, value string) {
	cli := b.connected()
	if cli == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
		defer cancel()
		ps, err := cli.SetPrivacySetting(ctx, types.PrivacySettingType(key), types.PrivacySetting(value))
		if err != nil {
			b.log.Warnf("set privacy %s=%s: %v", key, value, err)
			b.emit(model.NoticeEvent{Text: "Couldn't change the privacy setting."})
			b.emit(model.AccountEvent{}) // back to the old value
			return
		}
		b.saveAccount(func(a *model.Account) { a.Privacy = privacyMap(&ps) })
	}()
}

// SetDefaultTimer implements model.Backend.
func (b *Backend) SetDefaultTimer(d time.Duration) {
	cli := b.connected()
	if cli == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
		defer cancel()
		if err := cli.SetDefaultDisappearingTimer(ctx, d); err != nil {
			b.log.Warnf("set default timer: %v", err)
			b.emit(model.NoticeEvent{Text: "Couldn't change the default message timer."})
			b.emit(model.AccountEvent{})
			return
		}
		_ = b.store.setMetaValue(ctx, defaultTimerKey, strconv.Itoa(int(d.Seconds())))
		b.emit(model.AccountEvent{})
	}()
}
