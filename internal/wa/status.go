package wa

import (
	"context"
	"os"
	"sort"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waWeb"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// statusChat is the pseudo-chat status updates are posted to.
var statusChat = types.StatusBroadcastJID.String()

// statusTTL is how long a status stays visible.
const statusTTL = 24 * time.Hour

func isStatus(j types.JID) bool {
	return j.Server == types.BroadcastServer && j.User == types.StatusBroadcastJID.User
}

// storedStatus is one status update as kept in wz_status.
type storedStatus struct {
	id, sender, push string
	fromMe           bool
	ts               time.Time
	c                content
	viewed           bool
	revoke           string // ID of a status that was deleted
}

// parseStatus interprets a message posted to status@broadcast.
func (b *Backend) parseStatus(ctx context.Context, evt *events.Message) (storedStatus, bool) {
	m := evt.Message
	if m == nil {
		return storedStatus{}, false
	}
	if pm := m.GetProtocolMessage(); pm != nil {
		if pm.GetType() == waE2E.ProtocolMessage_REVOKE && pm.GetKey().GetID() != "" {
			return storedStatus{revoke: pm.GetKey().GetID()}, true
		}
		return storedStatus{}, false
	}
	c := describe(m)
	if c.text == "" && c.media == model.MediaNone {
		return storedStatus{}, false
	}
	s := storedStatus{
		id:     evt.Info.ID,
		sender: b.canonical(ctx, evt.Info.Sender).String(),
		push:   evt.Info.PushName,
		fromMe: evt.Info.IsFromMe,
		ts:     evt.Info.Timestamp,
		c:      c,
	}
	if w := evt.SourceWebMsg; w != nil && !s.fromMe {
		st := w.GetStatus()
		s.viewed = st == waWeb.WebMessageInfo_READ || st == waWeb.WebMessageInfo_PLAYED
	}
	return s, true
}

func (s *msgStore) putStatus(ctx context.Context, x execer, st storedStatus) error {
	if st.revoke != "" {
		_, err := x.ExecContext(ctx, `DELETE FROM wz_status WHERE id = ?`, st.revoke)
		return err
	}
	_, err := x.ExecContext(ctx, `
		INSERT INTO wz_status (id, sender, push, from_me, ts, media, text, bg, thumb, media_blob, viewed)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET viewed = MAX(wz_status.viewed, excluded.viewed),
			thumb = COALESCE(excluded.thumb, wz_status.thumb)`,
		st.id, st.sender, st.push, boolInt(st.fromMe), st.ts.Unix(), int(st.c.media), st.c.text, int64(st.c.bg),
		st.c.thumb, st.c.blob, boolInt(st.viewed))
	return err
}

func (s *msgStore) setStatusViewed(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	_, err := s.db.ExecContext(ctx, `UPDATE wz_status SET viewed = 1 WHERE id IN (`+placeholders(len(ids))+`)`, args...)
	return err
}

// statusMessage rebuilds a stored status update for a reply's context:
// media keep their original (downloadable) message, text keeps its
// background color.
func (s *msgStore) statusMessage(ctx context.Context, id string) *waE2E.Message {
	var (
		media int
		text  string
		bg    int64
		blob  []byte
	)
	err := s.db.QueryRowContext(ctx, `SELECT media, text, bg, media_blob FROM wz_status WHERE id = ?`, id).
		Scan(&media, &text, &bg, &blob)
	if err != nil {
		return &waE2E.Message{Conversation: proto.String("")}
	}
	if len(blob) > 0 {
		if m := mediaMessage(model.Media(media), blob); m != nil {
			return m
		}
	}
	et := &waE2E.ExtendedTextMessage{Text: proto.String(text)}
	if bg != 0 {
		et.BackgroundArgb = proto.Uint32(uint32(bg))
	}
	return &waE2E.Message{ExtendedTextMessage: et}
}

type statusRow struct {
	sender, push string
	fromMe       bool
	u            *model.StatusUpdate
}

// dropStatuses deletes updates older than before and returns their IDs.
func (s *msgStore) dropStatuses(ctx context.Context, before time.Time) []string {
	rows, err := s.db.QueryContext(ctx, `DELETE FROM wz_status WHERE ts < ? RETURNING id`, before.Unix())
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// recentStatuses returns updates newer than since, oldest first.
func (s *msgStore) recentStatuses(ctx context.Context, since time.Time) ([]statusRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, sender, push, from_me, ts, media, text, bg, thumb, viewed
		FROM wz_status WHERE ts >= ? ORDER BY ts`, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []statusRow
	for rows.Next() {
		var (
			r                     statusRow
			u                     model.StatusUpdate
			fromMe, media, viewed int
			ts, bg                int64
		)
		if err := rows.Scan(&u.ID, &r.sender, &r.push, &fromMe, &ts, &media, &u.Text, &bg, &u.Thumb, &viewed); err != nil {
			return nil, err
		}
		u.Time = time.Unix(ts, 0)
		u.Media = model.Media(media)
		u.Background = uint32(bg)
		u.Viewed = viewed != 0 && fromMe == 0 // your own ring stays green
		r.fromMe = fromMe != 0
		r.u = &u
		out = append(out, r)
	}
	return out, rows.Err()
}

func (b *Backend) onStatus(e *events.Message) {
	st, ok := b.parseStatus(b.ctx, e)
	if !ok {
		return
	}
	if err := b.store.putStatus(b.ctx, b.db, st); err != nil {
		b.log.Warnf("store status %s: %v", st.id, err)
		return
	}
	b.emit(model.StatusEvent{})
}

// Statuses implements model.Backend.
func (b *Backend) Statuses() []*model.StatusThread {
	ctx := b.ctx
	since := time.Now().Add(-statusTTL)
	// Old updates go, with their downloaded pictures and videos.
	for _, id := range b.store.dropStatuses(ctx, since) {
		path := b.mediaPath(statusChat, id)
		for _, p := range []string{path, path + ".failed", path + ".mp4"} {
			_ = os.Remove(p)
		}
	}
	rows, err := b.store.recentStatuses(ctx, since)
	if err != nil {
		b.log.Errorf("load statuses: %v", err)
		return nil
	}
	byID := map[string]*model.StatusThread{}
	var threads []*model.StatusThread
	for _, r := range rows {
		id := r.sender
		if r.fromMe {
			id = "me"
		}
		t := byID[id]
		if t == nil {
			t = &model.StatusThread{ID: r.sender, Mine: r.fromMe}
			if !r.fromMe {
				j, _ := types.ParseJID(r.sender)
				t.Name = b.statusName(ctx, j, r.push)
			}
			byID[id] = t
			threads = append(threads, t)
		}
		t.Updates = append(t.Updates, r.u)
	}
	sort.SliceStable(threads, func(i, j int) bool {
		a, c := threads[i], threads[j]
		if a.Mine != c.Mine {
			return a.Mine
		}
		return a.Last().Time.After(c.Last().Time)
	})
	return threads
}

// statusName labels a status poster: saved name, business name, push name
// (without the "~" group chats use), then the phone number.
func (b *Backend) statusName(ctx context.Context, j types.JID, push string) string {
	n := b.lookup(ctx, j)
	return first(n.saved, n.business, n.push, push, n.phone, n.redacted, j.User)
}

// ViewStatus marks a status as seen and tells its poster.
func (b *Backend) ViewStatus(threadID, statusID string) {
	_ = b.store.setStatusViewed(b.ctx, []string{statusID})
	cli := b.client()
	sender, err := types.ParseJID(threadID)
	if cli == nil || err != nil || !cli.IsConnected() {
		return
	}
	go func() {
		if err := cli.MarkRead(b.ctx, []types.MessageID{statusID}, time.Now(), types.StatusBroadcastJID, sender); err != nil {
			b.log.Debugf("mark status read: %v", err)
		}
	}()
}
