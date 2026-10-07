package wa

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// PostStatus implements model.Backend. The update is stored in wz_status
// at once, so it shows in your own thread while it uploads; if it can't be
// sent, it is taken out again.
func (b *Backend) PostStatus(p model.StatusPost) {
	cli := b.connected()
	if cli == nil || cli.Store.ID == nil {
		return
	}
	if p.GroupID != "" {
		j, err := types.ParseJID(p.GroupID)
		if err != nil || j.Server != types.GroupServer {
			b.emit(model.NoticeEvent{Text: "Invalid group for status."})
			return
		}
	}
	if p.File == nil && strings.TrimSpace(p.Text) == "" {
		return
	}
	st := storedStatus{
		group:  p.GroupID,
		id:     cli.GenerateMessageID(),
		sender: b.ownJID("").String(),
		fromMe: true,
		ts:     time.Now(),
		c:      content{text: p.Text, bg: p.Background},
	}
	if p.File == nil {
		msg := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text:           proto.String(p.Text),
			TextArgb:       proto.Uint32(0xffffffff),
			BackgroundArgb: proto.Uint32(p.Background),
			Font:           waE2E.ExtendedTextMessage_SYSTEM.Enum(),
		}}
		if !b.storeStatus(st) {
			return
		}
		go b.sendStatus(cli, st.id, msg, p.GroupID)
		return
	}

	a := *p.File
	name := filepath.Base(a.Path)
	m := &model.Message{ID: st.id, ChatID: statusChat, FromMe: true, Kind: model.KindImage, Media: a.Media,
		Text: p.Text, FileName: name, FileType: fileType(a)}
	up := upload{path: a.Path}
	switch a.Media {
	case model.MediaImage:
		if err := up.prepareImage(a.Quality); err != nil {
			b.emit(model.NoticeEvent{Text: "Couldn't read the photo " + name + "."})
			return
		}
		m.Thumb, m.FileType = up.thumb, "image/jpeg"
		if up.png {
			m.FileType = "image/png"
		}
		// Your own update shows without a download.
		up.keep(b.mediaPath(statusChat, m.ID))
	case model.MediaVideo:
		if _, err := os.Stat(a.Path); err != nil {
			b.emit(model.NoticeEvent{Text: "Couldn't read " + name + "."})
			return
		}
		m.Duration = mp4Seconds(a.Path)
	case model.MediaVoice, model.MediaAudio:
		if p.GroupID == "" {
			b.emit(model.NoticeEvent{Text: "Audio posting is available for group status."})
			return
		}
		if _, err := os.Stat(a.Path); err != nil {
			b.emit(model.NoticeEvent{Text: "Couldn't read " + name + "."})
			return
		}
	default:
		b.emit(model.NoticeEvent{Text: "This file cannot be posted as a status."})
		return
	}
	st.c.media, st.c.thumb, st.c.duration = a.Media, m.Thumb, m.Duration
	st.c.file.Type = m.FileType
	if !b.storeStatus(st) {
		return
	}
	go func() {
		if m.Media == model.MediaVideo || m.Media == model.MediaAudio || m.Media == model.MediaVoice {
			if st, err := os.Stat(a.Path); err == nil && st.Size() <= maxLocalCopy {
				// Your own update plays without a download. Copied here,
				// not on the UI goroutine that posts it.
				up.copyTo(b.mediaFilePath(m))
			}
		}
		ctx, cancel := context.WithTimeout(b.ctx, 30*time.Minute)
		defer cancel()
		msg, inner := fileMessage(m, up, p.Text, nil, false)
		res, err := up.push(ctx, cli, m.Media)
		if err != nil {
			b.statusFailed(st.id, err)
			return
		}
		uploaded(inner, res)
		// Kept so a reply to it can quote it.
		if _, err := b.db.ExecContext(b.ctx, `UPDATE wz_status SET media_blob = ? WHERE id = ?`, marshal(inner), st.id); err != nil {
			b.log.Warnf("store status media %s: %v", st.id, err)
		}
		b.emit(model.MediaEvent{ChatID: statusChat, MsgID: st.id})
		b.sendStatus(cli, st.id, msg, p.GroupID)
	}()
}

// pendingStatus marks, in wz_meta, an update of yours that hasn't been
// sent yet. If the app quits before it is, dropPendingStatuses takes it
// out on the next start, so it doesn't show as posted.
const pendingStatus = "status-pending:"

// storeStatus keeps your new update and announces it.
func (b *Backend) storeStatus(st storedStatus) bool {
	err := b.store.setMetaValue(b.ctx, pendingStatus+st.id, "1")
	if err == nil {
		err = b.store.putStatus(b.ctx, b.db, st)
	}
	if err != nil {
		b.log.Errorf("store status %s: %v", st.id, err)
		b.emit(model.NoticeEvent{Text: "Couldn't post your status."})
		return false
	}
	b.emit(model.StatusEvent{})
	return true
}

// sendStatus sends a status update to the contacts your status privacy
// lets see it (hypermeow works them out for status@broadcast).
func (b *Backend) sendStatus(cli *whatsmeow.Client, id string, msg *waE2E.Message, group string) {
	if !cli.IsConnected() {
		b.statusFailed(id, errors.New("offline"))
		return
	}
	to := types.StatusBroadcastJID
	extra := whatsmeow.SendRequestExtra{ID: id}
	if group != "" {
		to, _ = types.ParseJID(group) // validated by PostStatus
		msg, extra = groupStatusMessage(msg)
		extra.ID = id
	}
	if _, err := cli.SendMessage(b.ctx, to, msg, extra); err != nil {
		b.statusFailed(id, err)
		return
	}
	if _, err := b.db.ExecContext(b.ctx, `DELETE FROM wz_meta WHERE key = ?`, pendingStatus+id); err != nil {
		b.log.Warnf("status %s sent: %v", id, err)
	}
}

// statusFailed takes an update that couldn't be sent back out. When the
// app is closing, the database may be gone already; the update is still
// marked pending then, and goes on the next start.
func (b *Backend) statusFailed(id string, err error) {
	b.log.Errorf("post status %s: %v", id, err)
	b.dropStatus(b.ctx, id)
	b.emit(model.NoticeEvent{Text: "Couldn't post your status."})
	b.emit(model.StatusEvent{})
}

// dropStatus deletes an unsent update of yours, its media and its mark.
func (b *Backend) dropStatus(ctx context.Context, id string) {
	if _, err := b.db.ExecContext(ctx, `DELETE FROM wz_status WHERE id = ?`, id); err != nil {
		return // keep the mark, to try again on the next start
	}
	path := b.mediaPath(statusChat, id)
	files, _ := filepath.Glob(path + ".*")
	for _, p := range append(files, path) {
		_ = os.Remove(p)
	}
	_, _ = b.db.ExecContext(ctx, `DELETE FROM wz_meta WHERE key = ?`, pendingStatus+id)
}

// dropPendingStatuses takes out the updates the app quit before sending.
func (b *Backend) dropPendingStatuses(ctx context.Context) {
	rows, err := b.db.QueryContext(ctx, `SELECT key FROM wz_meta WHERE key LIKE ?`, pendingStatus+"%")
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			ids = append(ids, strings.TrimPrefix(k, pendingStatus))
		}
	}
	rows.Close()
	for _, id := range ids {
		b.log.Warnf("status %s was never sent; dropping it", id)
		b.dropStatus(ctx, id)
	}
}

// statusPrivacy caches your status privacy setting.
type statusPrivacy struct {
	mu       sync.Mutex
	val      *model.StatusPrivacy
	at       time.Time
	fetching bool
}

// statusPrivacyTTL is how long the setting is trusted before it's fetched
// again (it changes on the phone).
const statusPrivacyTTL = time.Minute

// StatusPrivacy implements model.Backend.
func (b *Backend) StatusPrivacy() *model.StatusPrivacy {
	sp := &b.statusPriv
	sp.mu.Lock()
	defer sp.mu.Unlock()
	cli := b.client()
	if !sp.fetching && time.Since(sp.at) > statusPrivacyTTL && cli != nil && cli.IsConnected() {
		sp.fetching = true
		go func() {
			ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
			defer cancel()
			list, err := cli.GetStatusPrivacy(ctx)
			sp.mu.Lock()
			sp.fetching = false
			if err != nil || len(list) == 0 {
				sp.mu.Unlock()
				b.log.Warnf("get status privacy: %v", err)
				return
			}
			sp.val, sp.at = audienceOf(list[0]), time.Now()
			sp.mu.Unlock()
			b.emit(model.StatusEvent{})
		}()
	}
	if sp.val == nil {
		return nil
	}
	v := *sp.val
	return &v
}

// audienceOf converts the default status privacy list.
func audienceOf(p types.StatusPrivacy) *model.StatusPrivacy {
	out := &model.StatusPrivacy{Count: len(p.List)}
	switch p.Type {
	case types.StatusPrivacyTypeBlacklist:
		out.Audience = model.AudienceExcept
	case types.StatusPrivacyTypeWhitelist:
		out.Audience = model.AudienceOnly
	default:
		out.Audience, out.Count = model.AudienceContacts, 0
	}
	return out
}
