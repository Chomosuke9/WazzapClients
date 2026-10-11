package wa

import (
	"context"
	"sort"
	"time"

	waBinary "github.com/polymorfa/hypermeow/binary"
	"github.com/polymorfa/hypermeow/types"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Message info: when each person one of your messages went to got it,
// read it and played it, kept in wz_receipts. In a group, the message's
// own receipt (its ticks) only moves on once every member has.

// receiptsSchema has no rowid: the table is only ever looked up by its
// primary key, and one with a rowid keeps every key a second time, in the
// index that enforces it.
const receiptsSchema = `CREATE TABLE IF NOT EXISTS wz_receipts (
	chat      TEXT NOT NULL,
	id        TEXT NOT NULL,              -- one of your messages
	who       TEXT NOT NULL,              -- who it went to, usually a LID
	delivered INTEGER NOT NULL DEFAULT 0, -- unix seconds, 0 = not yet
	read      INTEGER NOT NULL DEFAULT 0,
	played    INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (chat, id, who)
) WITHOUT ROWID;`

// personReceipt is one row of wz_receipts, in unix seconds.
type personReceipt struct {
	chat, id, who           string
	delivered, read, played int64
}

// putReceipt records what a receipt says, keeping the earliest time of
// each step. Read implies delivered, and played implies read.
func (s *msgStore) putReceipt(ctx context.Context, x execer, r personReceipt) error {
	if r.played != 0 && (r.read == 0 || r.read > r.played) {
		r.read = r.played
	}
	if r.read != 0 && (r.delivered == 0 || r.delivered > r.read) {
		r.delivered = r.read
	}
	_, err := x.ExecContext(ctx, `INSERT INTO wz_receipts (chat, id, who, delivered, read, played) VALUES (?1, ?2, ?3, ?4, ?5, ?6)
		ON CONFLICT (chat, id, who) DO UPDATE SET
			delivered = CASE WHEN delivered = 0 OR (excluded.delivered != 0 AND excluded.delivered < delivered) THEN excluded.delivered ELSE delivered END,
			read = CASE WHEN read = 0 OR (excluded.read != 0 AND excluded.read < read) THEN excluded.read ELSE read END,
			played = CASE WHEN played = 0 OR (excluded.played != 0 AND excluded.played < played) THEN excluded.played ELSE played END`,
		r.chat, r.id, r.who, r.delivered, r.read, r.played)
	return err
}

// receipts returns a message's rows.
func (s *msgStore) receipts(ctx context.Context, chat, id string) []personReceipt {
	rows, err := s.db.QueryContext(ctx, `SELECT who, delivered, read, played FROM wz_receipts WHERE chat = ? AND id = ?`, chat, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []personReceipt
	for rows.Next() {
		r := personReceipt{chat: chat, id: id}
		if rows.Scan(&r.who, &r.delivered, &r.read, &r.played) != nil {
			break
		}
		out = append(out, r)
	}
	return out
}

// userReceipt is one person's step in a group's grouped receipt.
type userReceipt struct {
	id, typ string
	who     types.JID
	at      int64
}

// groupedReceipts reads a group's grouped receipts itself. Without a
// participant, a <receipt> holds many people's receipts of one of your
// messages: <participants message_id="…"> with a <user jid="…" t="…"
// type="delivery|read|played|inactive"/> each. hypermeow reads the
// message's ID from a "key" attribute and gives every user the receipt's
// own type, so their receipts were stored under no message at all, and
// Message info missed most of a big group. The node goes on to hypermeow
// without its users, which it still acks.
func (b *Backend) groupedReceipts(_ context.Context, node *waBinary.Node) (*waBinary.Node, bool) {
	if node.Tag != "receipt" {
		return nil, false
	}
	if _, ok := node.Attrs["participant"]; ok {
		return nil, false
	}
	parts := node.GetChildrenByTag("participants")
	if len(parts) == 0 {
		return nil, false
	}
	ag := node.AttrGetter()
	chat := ag.JID("from")
	if !ag.OK() || chat.Server != types.GroupServer {
		return nil, false
	}
	var rs []userReceipt
	kept := make([]waBinary.Node, len(parts))
	for i, p := range parts {
		kept[i] = waBinary.Node{Tag: p.Tag, Attrs: p.Attrs}
		pag := p.AttrGetter()
		id := pag.OptionalString("message_id")
		if id == "" {
			id = pag.OptionalString("key")
		}
		if id == "" {
			continue
		}
		for _, u := range p.GetChildrenByTag("user") {
			uag := u.AttrGetter()
			r := userReceipt{id: id, who: uag.JID("jid"), at: uag.UnixTime("t").Unix(), typ: uag.OptionalString("type")}
			if uag.OK() {
				rs = append(rs, r)
			}
		}
	}
	if len(rs) > 0 {
		b.goWork(func() { b.onGroupedReceipts(chat.String(), rs) })
	}
	out := *node
	out.Content = kept
	return &out, false
}

// onGroupedReceipts stores what a grouped receipt says, then moves the
// ticks of its messages on, once.
func (b *Backend) onGroupedReceipts(chat string, rs []userReceipt) {
	ctx := b.ctx
	// Names first: the device store must not be read inside a wz_* write
	// transaction.
	var prs []personReceipt
	for _, r := range rs {
		pr := personReceipt{chat: chat, id: r.id, delivered: r.at}
		switch types.ReceiptType(r.typ) {
		case "delivery", types.ReceiptTypeDelivered:
		case types.ReceiptTypeRead:
			pr.read = r.at
		case types.ReceiptTypePlayed:
			pr.played = r.at
		default:
			// "inactive": one of their devices that hasn't been online
			// for a while, which doesn't mean they got it.
			continue
		}
		if b.isMe(r.who) {
			continue
		}
		pr.who = b.canonical(ctx, r.who).String()
		prs = append(prs, pr)
	}
	if len(prs) == 0 {
		return
	}
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		b.log.Warnf("store receipts: %v", err)
		return
	}
	defer tx.Rollback()
	var ids []string
	seen := map[string]bool{}
	for _, pr := range prs {
		if err := b.store.putReceipt(ctx, tx, pr); err != nil {
			b.log.Warnf("store receipt: %v", err)
			return
		}
		if !seen[pr.id] {
			seen[pr.id] = true
			ids = append(ids, pr.id)
		}
	}
	if err := tx.Commit(); err != nil {
		b.log.Warnf("store receipts: %v", err)
		return
	}
	b.setTicks(ctx, chat, ids, model.Delivered, true)
}

// otherMembers returns a group's stored members other than you.
func (b *Backend) otherMembers(ctx context.Context, chat string) []types.GroupParticipant {
	var out []types.GroupParticipant
	for _, p := range b.store.members(ctx, chat) {
		if b.isMe(p.JID) || (!p.PhoneNumber.IsEmpty() && b.isMe(p.PhoneNumber)) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// groupReceipt is what a group message's ticks show: Read once every
// member read it, Delivered once every member got it. ok is false while
// the group's members aren't known.
func (b *Backend) groupReceipt(ctx context.Context, chat, id string) (r model.Receipt, ok bool) {
	members := b.otherMembers(ctx, chat)
	if len(members) == 0 {
		return 0, false
	}
	got := map[string]personReceipt{}
	for _, pr := range b.store.receipts(ctx, chat, id) {
		got[pr.who] = pr
	}
	delivered, read := true, true
	for _, p := range members {
		pr, ok := got[b.canonical(ctx, p.JID).String()]
		if !ok && !p.PhoneNumber.IsEmpty() {
			pr, ok = got[p.PhoneNumber.ToNonAD().String()]
		}
		delivered = delivered && ok && pr.delivered != 0
		read = read && ok && pr.read != 0
	}
	switch {
	case read:
		return model.Read, true
	case delivered:
		return model.Delivered, true
	}
	return model.Sent, true
}

// MessageInfo implements model.Backend.
func (b *Backend) MessageInfo(m *model.Message) *model.MessageInfo {
	ctx := b.ctx
	info := &model.MessageInfo{Members: 1}
	rows := b.store.receipts(ctx, m.ChatID, m.ID)
	chat, _ := types.ParseJID(m.ChatID)
	group := chat.Server == types.GroupServer
	pns := map[string]types.JID{}
	if group {
		members := b.otherMembers(ctx, m.ChatID)
		if len(members) > 0 {
			info.Members = len(members)
		}
		for _, p := range members {
			pns[b.canonical(ctx, p.JID).String()] = p.PhoneNumber
		}
	}
	at := func(sec int64) time.Time {
		if sec == 0 {
			return time.Time{}
		}
		return time.Unix(sec, 0)
	}
	for _, r := range rows {
		j, err := types.ParseJID(r.who)
		if err != nil || b.isMe(j) {
			continue
		}
		name := b.memberName(ctx, j, pns[r.who])
		if !group {
			name = b.chatName(ctx, j)
		}
		info.Receipts = append(info.Receipts, model.PersonReceipt{
			ID: r.who, Name: name,
			Delivered: at(r.delivered), Read: at(r.read), Played: at(r.played),
		})
	}
	if len(info.Receipts) > info.Members {
		info.Members = len(info.Receipts) // someone who has left since
	}
	sort.SliceStable(info.Receipts, func(i, j int) bool {
		a, c := info.Receipts[i], info.Receipts[j]
		if a.Read.IsZero() != c.Read.IsZero() {
			return !a.Read.IsZero()
		}
		if !a.Read.Equal(c.Read) {
			return a.Read.Before(c.Read)
		}
		return a.Delivered.Before(c.Delivered)
	})
	return info
}
