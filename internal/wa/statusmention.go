package wa

import (
	"context"

	"github.com/polymorfa/hypermeow/proto/waE2E"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// A status update that mentions someone, or a group, doesn't say so
// itself. Its poster's phone sends whoever it mentions a message of its
// own: a statusMentionMessage in your chat with them, or a
// groupStatusMentionMessage to the group, each a protocol message
// (STATUS_MENTION_MESSAGE) whose key names the status (remoteJid
// status@broadcast). WhatsApp shows it as a card ("Status <poster>",
// "@ This group was mentioned.") with the status's picture, and the
// status viewer says "Mentioned privately" over that status.

// statusMentionsSchema points at those messages from the statuses they
// name, for the viewer. The status may come after the message, or never
// (the poster may hide it from you), so it isn't checked.
const statusMentionsSchema = `CREATE TABLE IF NOT EXISTS wz_status_mentions (
	status TEXT NOT NULL, -- a status update's ID
	chat   TEXT NOT NULL, -- the message that says it mentioned you or a group
	id     TEXT NOT NULL,
	PRIMARY KEY (status, chat, id)
) WITHOUT ROWID;`

// statusMentionOf reads the status a status mention message names, or
// returns nil when m (unwrapped) is none.
func statusMentionOf(m *waE2E.Message) *model.StatusMention {
	fp, group := m.GetStatusMentionMessage(), false
	if g := m.GetGroupStatusMentionMessage(); g != nil {
		fp, group = g, true
	}
	pm := fp.GetMessage().GetProtocolMessage()
	if pm.GetType() != waE2E.ProtocolMessage_STATUS_MENTION_MESSAGE || pm.GetKey().GetID() == "" {
		return nil
	}
	return &model.StatusMention{StatusID: pm.GetKey().GetID(), Group: group}
}

// noteStatusMention points at a stored status mention message from the
// status it names.
func (b *Backend) noteStatusMention(ctx context.Context, r rawMsg) {
	sm := r.StatusMention
	if sm == nil {
		return
	}
	res, err := b.db.ExecContext(ctx, `INSERT OR IGNORE INTO wz_status_mentions (status, chat, id) VALUES (?, ?, ?)`,
		sm.StatusID, r.ChatID, r.ID)
	if err != nil {
		b.log.Warnf("store status mention: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		b.emit(model.StatusEvent{})
	}
}

// fillStatusMention gives a status mention card what its status shows,
// while the status is kept.
func (s *msgStore) fillStatusMention(ctx context.Context, sm *model.StatusMention) {
	var media int
	var bg int64
	err := s.db.QueryRowContext(ctx, `SELECT media, text, bg, thumb FROM wz_status WHERE id = ?`, sm.StatusID).
		Scan(&media, &sm.Text, &bg, &sm.Thumb)
	if err != nil {
		return
	}
	sm.Found, sm.Media, sm.Background = true, model.Media(media), uint32(bg)
}

// statusMentions maps the statuses that mentioned you or a group of yours
// to the message that says so (the latest, when there are several).
func (s *msgStore) statusMentions(ctx context.Context) map[string][2]string {
	rows, err := s.db.QueryContext(ctx, `SELECT status, chat, id FROM wz_status_mentions`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := map[string][2]string{}
	for rows.Next() {
		var st, chat, id string
		if rows.Scan(&st, &chat, &id) == nil {
			out[st] = [2]string{chat, id}
		}
	}
	return out
}
