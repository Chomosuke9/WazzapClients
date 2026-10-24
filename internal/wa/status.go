package wa

import (
	"context"
	"os"
	"path/filepath"
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
	participant      string // the poster as the status came (sender is canonical), for its read receipt
	group            string
	fromMe           bool
	ts               time.Time
	c                content
	viewed           bool
	revoke           string // ID of a status that was deleted (ts and fromMe are the delete's)
	audience, emoji  string // the poster's audience it was shared with (audienceLabel)
}

// parseStatus interprets a message posted to status@broadcast.
func (b *Backend) parseStatus(ctx context.Context, evt *events.Message) (storedStatus, bool) {
	m := unwrap(evt.Message)
	if m == nil {
		return storedStatus{}, false
	}
	if pm := m.GetProtocolMessage(); pm != nil {
		if pm.GetType() == waE2E.ProtocolMessage_REVOKE && pm.GetKey().GetID() != "" {
			return storedStatus{revoke: pm.GetKey().GetID(), fromMe: evt.Info.IsFromMe, ts: evt.Info.Timestamp}, true
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
	if !evt.Info.Sender.IsEmpty() {
		s.participant = evt.Info.Sender.ToNonAD().String()
	}
	s.emoji, s.audience = audienceLabel(c.ctx.GetStatusAudienceMetadata())
	if isGroupStatus(evt) {
		s.group = evt.Info.Chat.String()
		for _, a := range c.ctx.GetStatusAttributions() {
			if author, err := types.ParseJID(a.GetGroupStatus().GetAuthorJID()); err == nil && evt.Info.Sender.IsEmpty() && (author.Server == types.HiddenUserServer || author.Server == types.DefaultUserServer) {
				s.sender = b.canonical(ctx, author).String()
				s.participant = author.ToNonAD().String()
				break
			}
		}
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
	var blob []byte
	if st.c.inner != nil {
		blob = marshal(st.c.inner)
	}
	_, err := x.ExecContext(ctx, `
		INSERT INTO wz_status (id, sender, push, from_me, ts, media, text, bg, thumb, media_blob, viewed, group_jid, duration, file_type, participant,
			audience, audience_emoji)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET viewed = MAX(wz_status.viewed, excluded.viewed),
			thumb = COALESCE(excluded.thumb, wz_status.thumb),
			participant = COALESCE(NULLIF(wz_status.participant, ''), excluded.participant)`,
		st.id, st.sender, st.push, boolInt(st.fromMe), st.ts.Unix(), int(st.c.media), st.c.text, int64(st.c.bg),
		st.c.thumb, blob, boolInt(st.viewed), st.group, st.c.duration, st.c.file.Type, st.participant, st.audience, st.emoji)
	return err
}

// markStatusRevoked flags a status its poster deleted at at, keeping it
// (model.PrefKeepDeleted).
func (s *msgStore) markStatusRevoked(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE wz_status SET revoked = ? WHERE id = ? AND revoked = 0`, at.UnixMilli(), id)
	return err
}

// statusSeenByMe reports whether a receipt says you saw what it names on
// another device. Others' reads of a status are of your own.
func statusSeenByMe(e *events.Receipt) bool {
	switch e.Type {
	case types.ReceiptTypeReadSelf, types.ReceiptTypePlayedSelf:
		return true
	case types.ReceiptTypeRead, types.ReceiptTypePlayed:
		return e.IsFromMe
	}
	return false
}

// statusesSeen marks the statuses among ids viewed: those of group (a
// group's stories), or of status@broadcast when group is "". It returns
// how many of ids are such statuses.
func (s *msgStore) statusesSeen(ctx context.Context, group string, ids []types.MessageID) int64 {
	if len(ids) == 0 {
		return 0
	}
	args := []any{group}
	for _, id := range ids {
		args = append(args, id)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE wz_status SET viewed = 1 WHERE group_jid = ? AND id IN (`+placeholders(len(ids))+`)`, args...)
	if err != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return n
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
	group        string
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
	rows, err := s.db.QueryContext(ctx, `SELECT id, sender, push, from_me, ts, media, text, bg, thumb, viewed, revoked, group_jid, duration, file_type,
			audience, audience_emoji
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
			ts, bg, revoked       int64
		)
		if err := rows.Scan(&u.ID, &r.sender, &r.push, &fromMe, &ts, &media, &u.Text, &bg, &u.Thumb, &viewed, &revoked, &r.group, &u.Duration, &u.FileType,
			&u.Audience, &u.AudienceEmoji); err != nil {
			return nil, err
		}
		u.Time = time.Unix(ts, 0)
		u.Media = model.Media(media)
		u.Background = uint32(bg)
		if revoked != 0 {
			u.Revoked = time.UnixMilli(revoked)
		}
		// Your own ring stays green; your updates in a group's are seen
		// once you watch them, like anyone's.
		u.Viewed = viewed != 0 && (fromMe == 0 || r.group != "")
		u.FromMe = fromMe != 0
		u.SenderID = r.sender
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
	// Like revoke: with Keep deleted messages on, someone else's deleted
	// status stays (until it expires) and is only flagged.
	if st.revoke != "" && !st.fromMe && b.Pref(model.PrefKeepDeleted) == "on" {
		at := st.ts
		if at.IsZero() {
			at = time.Now()
		}
		if err := b.store.markStatusRevoked(b.ctx, st.revoke, at); err != nil {
			b.log.Warnf("flag deleted status %s: %v", st.revoke, err)
			return
		}
		b.emit(model.StatusEvent{})
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
		files, _ := filepath.Glob(path + ".*") // hashed cache key, including audio extensions
		for _, p := range append(files, path) {
			_ = os.Remove(p)
		}
	}
	rows, err := b.store.recentStatuses(ctx, since)
	if err != nil {
		b.log.Errorf("load statuses: %v", err)
		return nil
	}
	mutes := b.statusMutes(ctx)
	byID := map[string]*model.StatusThread{}
	skip := map[string]bool{}
	var threads []*model.StatusThread
	for _, r := range rows {
		id := r.sender
		if r.group != "" {
			id = r.group
		} else if r.fromMe {
			id = "me"
		}
		if skip[id] {
			continue
		}
		t := byID[id]
		if t == nil {
			t = &model.StatusThread{ID: r.sender, Mine: r.fromMe}
			if !r.fromMe {
				j, _ := types.ParseJID(r.sender)
				t.Name = b.statusName(ctx, j, r.push)
				if r.group == "" {
					if !b.savedContact(ctx, j) {
						// WhatsApp shows the status updates of your
						// saved contacts only.
						skip[id] = true
						continue
					}
					_, t.Muted = mutes[r.sender]
				}
			}
			if r.group != "" {
				t.ID, t.Group, t.Mine, t.Name = r.group, true, false, r.group
				if c, ok := b.store.chat(ctx, r.group); ok {
					t.Name = c.Name
				}
			}
			byID[id] = t
			threads = append(threads, t)
		}
		j, _ := types.ParseJID(r.sender)
		r.u.Sender = b.statusName(ctx, j, r.push)
		if r.fromMe {
			r.u.Sender = "You"
		}
		t.Updates = append(t.Updates, r.u)
	}
	sort.SliceStable(threads, func(i, j int) bool {
		a, c := threads[i], threads[j]
		if a.Mine != c.Mine {
			return a.Mine
		}
		return a.Latest().Time.After(c.Latest().Time)
	})
	return threads
}

// savedContact reports whether j is in your address book. Without a
// device store (before logging in) nobody can be told apart: everyone is.
func (b *Backend) savedContact(ctx context.Context, j types.JID) bool {
	return b.client() == nil || b.lookup(ctx, j).saved != ""
}

// statusName labels a status poster: saved name, business name, push name
// (without the "~" group chats use), then the phone number.
func (b *Backend) statusName(ctx context.Context, j types.JID, push string) string {
	n := b.lookup(ctx, j)
	return first(n.saved, n.business, n.push, push, n.phone, n.redacted, j.User)
}

// ViewStatus marks a status as seen and tells its poster, unless ghost
// mode is on: a read receipt to status@broadcast (a group story's group)
// naming the poster as the status came, which also shows it seen on your
// phone.
func (b *Backend) ViewStatus(threadID, statusID string) {
	_ = b.store.setStatusViewed(b.ctx, []string{statusID})
	cli := b.client()
	if cli == nil || !cli.IsConnected() || b.ghost() {
		return
	}
	var author, participant, group string
	var fromMe bool
	if err := b.db.QueryRowContext(b.ctx, `SELECT sender, participant, group_jid, from_me FROM wz_status WHERE id = ?`, statusID).
		Scan(&author, &participant, &group, &fromMe); err != nil || fromMe {
		return
	}
	chat := types.StatusBroadcastJID
	if group != "" {
		var err error
		if chat, err = types.ParseJID(group); err != nil {
			return
		}
	}
	sender, err := types.ParseJID(first(participant, author))
	if err != nil || sender.IsEmpty() {
		return
	}
	go func() {
		if err := cli.MarkRead(b.ctx, []types.MessageID{statusID}, time.Now(), chat, sender); err != nil {
			b.log.Debugf("mark status read: %v", err)
		}
	}()
}
