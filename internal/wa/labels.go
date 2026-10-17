package wa

import (
	"context"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Member labels are the tags group members give themselves ("Tyrant"),
// shown beside their name on their messages. Setting one sends the group a
// GROUP_MEMBER_LABEL_CHANGE protocol message (an empty label removes it),
// and history sync lists each participant's when a device links. There is
// no query for them: a label set before this app saw either never shows
// until it changes again. ContextInfo has a memberLabel too, though
// WhatsApp doesn't fill it on ordinary messages. The latest of them, by the
// time it was set, is kept per member in wz_member_labels.

type memberLabel struct {
	label string
	ts    int64
}

// labelsOf returns a group's labels by member, loading them once.
// b.labelMu must be held.
func (b *Backend) labelsOf(ctx context.Context, chat string) map[string]memberLabel {
	if m, ok := b.labels[chat]; ok {
		return m
	}
	m := map[string]memberLabel{}
	rows, err := b.store.db.QueryContext(ctx, `SELECT member, label, ts FROM wz_member_labels WHERE chat = ?`, chat)
	if err != nil {
		return m // not cached: tried again next time
	}
	defer rows.Close()
	for rows.Next() {
		var who string
		var l memberLabel
		if rows.Scan(&who, &l.label, &l.ts) == nil {
			m[who] = l
		}
	}
	if b.labels == nil {
		b.labels = map[string]map[string]memberLabel{}
	}
	b.labels[chat] = m
	return m
}

// setMemberLabel records member's label in a group, unless a newer one is
// known. It reports whether it changed.
func (b *Backend) setMemberLabel(ctx context.Context, chat string, member types.JID, ml *waE2E.MemberLabel) bool {
	if ml == nil || member.IsEmpty() {
		return false
	}
	ts := ml.GetLabelTimestamp()
	if ts > 1e11 {
		ts /= 1000 // milliseconds
	}
	who := b.canonical(ctx, member).String()
	b.labelMu.Lock()
	defer b.labelMu.Unlock()
	labels := b.labelsOf(ctx, chat)
	old, ok := labels[who]
	if ok && (ts < old.ts || ts == old.ts && old.label == ml.GetLabel()) {
		return false
	}
	_, err := b.store.db.ExecContext(ctx, `INSERT INTO wz_member_labels (chat, member, label, ts) VALUES (?, ?, ?, ?)
		ON CONFLICT (chat, member) DO UPDATE SET label = excluded.label, ts = excluded.ts`,
		chat, who, ml.GetLabel(), ts)
	if err != nil {
		return false
	}
	labels[who] = memberLabel{label: ml.GetLabel(), ts: ts}
	return old.label != ml.GetLabel()
}

// memberLabelOf returns a group member's latest label ("" once removed).
// ok is false when none is known, and the message's own label stands.
func (b *Backend) memberLabelOf(ctx context.Context, chat, member string) (label string, ok bool) {
	j, err := types.ParseJID(member)
	if err != nil || j.IsEmpty() {
		return "", false
	}
	who := b.canonical(ctx, j).String()
	b.labelMu.Lock()
	defer b.labelMu.Unlock()
	l, ok := b.labelsOf(ctx, chat)[who]
	return l.label, ok
}

// emitLabel tells the UI that member's label changed, under both of their
// IDs, since the messages it shows may carry either.
func (b *Backend) emitLabel(chat string, member types.JID, label string) {
	member = member.ToNonAD()
	ids := []string{member.String()}
	if c := b.canonical(b.ctx, member); c != member {
		ids = append(ids, c.String())
	}
	b.emit(model.LabelEvent{ChatID: chat, Members: ids, Label: label})
}
