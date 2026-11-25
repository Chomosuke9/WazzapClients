package wa

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Replies to a community's announcements ("comments" on the wire). Members
// can't write in the announcements, but they may reply to each one: the
// replies are text, show under it instead of in the chat, and notify no
// one. One is a message of its own, an encCommentMessage encrypted with
// the announcement's secret, whose <meta thread_msg_id> names the
// announcement. It's kept in wz_comments, not as a message.
//
// The columns are named apart from wz_messages', so that commentColumn can
// sit in any query of messages.
const commentsSchema = `
CREATE TABLE IF NOT EXISTS wz_comments (
	cchat    TEXT NOT NULL,
	cid      TEXT NOT NULL,              -- the reply's own ID
	cparent  TEXT NOT NULL,              -- the announcement
	cwho     TEXT NOT NULL,              -- usually a LID; meVoter for you
	cpush    TEXT NOT NULL DEFAULT '',
	cts      INTEGER NOT NULL,           -- unix seconds
	creceipt INTEGER NOT NULL DEFAULT 1, -- model.Receipt: of yours, whether it went
	crevoked INTEGER NOT NULL DEFAULT 0, -- unix ms it was deleted, kept with model.PrefKeepDeleted
	crevoked_by TEXT NOT NULL DEFAULT '', -- the admin who deleted it; '' for its sender
	cpayload BLOB NOT NULL,              -- the waE2E message the reply is, decrypted
	PRIMARY KEY (cchat, cid)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS wz_comments_parent ON wz_comments (cchat, cparent);
`

// commentColumn is how many replies an announcement has.
const commentColumn = `(SELECT count(*) FROM wz_comments WHERE cchat = chat AND cparent = id)`

// comment is a reply to an announcement, p.target.
type comment struct {
	id, who, push string
	at            time.Time
	receipt       model.Receipt
	payload       []byte
}

// putComment stores a reply to the announcement parent, or what came of
// yours since.
func (s *msgStore) putComment(ctx context.Context, x execer, chat, parent string, c comment) error {
	_, err := x.ExecContext(ctx, `INSERT INTO wz_comments (cchat, cid, cparent, cwho, cpush, cts, creceipt, cpayload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (cchat, cid) DO UPDATE SET creceipt = MAX(creceipt, excluded.creceipt),
			cpush = CASE WHEN excluded.cpush != '' THEN excluded.cpush ELSE cpush END`,
		chat, c.id, parent, c.who, c.push, c.at.Unix(), int(c.receipt), c.payload)
	return err
}

func (s *msgStore) setCommentReceipt(ctx context.Context, chat, id string, r model.Receipt) {
	_, _ = s.db.ExecContext(ctx, `UPDATE wz_comments SET creceipt = ? WHERE cchat = ? AND cid = ?`, int(r), chat, id)
}

// dropComment removes reply id, or with keep only flags it deleted at at
// by by (an admin, or "" for its sender), and returns the announcement
// it's on, or "" when id isn't a reply.
func (s *msgStore) dropComment(ctx context.Context, chat, id string, keep bool, at time.Time, by string) (parent string) {
	if keep {
		_ = s.db.QueryRowContext(ctx, `UPDATE wz_comments SET crevoked = CASE WHEN crevoked = 0 THEN ?1 ELSE crevoked END,
				crevoked_by = CASE WHEN crevoked = 0 THEN ?2 ELSE crevoked_by END
			WHERE cchat = ?3 AND cid = ?4 RETURNING cparent`, at.UnixMilli(), by, chat, id).Scan(&parent)
		return parent
	}
	_ = s.db.QueryRowContext(ctx, `DELETE FROM wz_comments WHERE cchat = ? AND cid = ? RETURNING cparent`, chat, id).Scan(&parent)
	return parent
}

// unmarkComment takes back dropComment's flag: the delete didn't go.
func (s *msgStore) unmarkComment(ctx context.Context, chat, id string) {
	_, _ = s.db.ExecContext(ctx, `UPDATE wz_comments SET crevoked = 0, crevoked_by = '' WHERE cchat = ? AND cid = ?`, chat, id)
}

// asAddonSenders calls f with evt as sent by each JID its encryption may
// name its sender by, until it works: what's added to an announcement is
// encrypted with its sender's LID, however the stanza names them.
func (b *Backend) asAddonSenders(cli *whatsmeow.Client, evt *events.Message, f func(*events.Message) error) error {
	err := f(evt)
	var also []types.JID
	if alt := evt.Info.SenderAlt; alt.Server == types.HiddenUserServer {
		also = append(also, alt)
	}
	if evt.Info.IsFromMe && cli.Store != nil && !cli.Store.LID.IsEmpty() {
		also = append(also, cli.Store.LID.ToNonAD())
	}
	for _, j := range also {
		if err == nil || j.User == evt.Info.Sender.User {
			continue
		}
		e := *evt
		e.Info.Sender = j
		err = f(&e)
	}
	return err
}

// parseComment reads the reply to an announcement in evt, encrypted or (in
// history) not, into p.
func (b *Backend) parseComment(ctx context.Context, evt *events.Message, p *parsed) bool {
	m := evt.Message
	var msg *waE2E.Message
	if enc := m.GetEncCommentMessage(); enc != nil {
		cli := b.client()
		if cli == nil {
			return false
		}
		p.target = enc.GetTargetMessageKey().GetID()
		err := b.asAddonSenders(cli, evt, func(e *events.Message) (err error) {
			msg, err = cli.DecryptComment(ctx, e)
			return err
		})
		if err != nil {
			b.log.Warnf("decrypt reply to %s in %s: %v", p.target, evt.Info.Chat, err)
			return false
		}
	} else if cm := m.GetCommentMessage(); cm != nil {
		p.target, msg = cm.GetTargetMessageKey().GetID(), cm.GetMessage()
	}
	if p.target == "" || evt.Info.ID == "" || describe(msg).text == "" {
		return false
	}
	p.comment = &comment{id: evt.Info.ID, who: b.reactorOf(ctx, evt.Info.Sender, evt.Info.IsFromMe), push: evt.Info.PushName,
		at: evt.Info.Timestamp, receipt: model.Sent, payload: marshal(msg)}
	return true
}

// Replies implements model.Backend.
func (b *Backend) Replies(m *model.Message) []model.Reply {
	ctx := b.ctx
	rows, err := b.db.QueryContext(ctx, `SELECT cid, cwho, cpush, cts, creceipt, crevoked, crevoked_by, cpayload FROM wz_comments
		WHERE cchat = ? AND cparent = ? ORDER BY cts, cid`, m.ChatID, m.ID)
	if err != nil {
		return nil
	}
	type row struct {
		model.Reply
		who, push, revokedBy string
		payload              []byte
	}
	var found []row
	for rows.Next() {
		var r row
		var ts, revoked int64
		var receipt int
		if rows.Scan(&r.ID, &r.who, &r.push, &ts, &receipt, &revoked, &r.revokedBy, &r.payload) != nil {
			break
		}
		r.Time, r.Receipt = time.Unix(ts, 0), model.Receipt(receipt)
		if revoked != 0 {
			r.Revoked = time.UnixMilli(revoked)
		}
		found = append(found, r)
	}
	rows.Close() // names below read the database too
	out := make([]model.Reply, 0, len(found))
	for _, r := range found {
		if r.Me = r.who == meVoter; r.Me {
			r.SenderID, r.Sender = b.ownJID(m.ChatID).String(), "You"
		} else {
			r.SenderID, r.Sender = r.who, b.senderNameStr(ctx, r.who, r.push)
		}
		r.DeletedBy, r.DeletedByMe = b.deleter(ctx, r.revokedBy)
		text, mentions := rawText(r.payload)
		if mentions != "" {
			text = b.replaceMentions(ctx, m.ChatID, text, mentions)
		} else if strings.Contains(text, "@") {
			text = b.guessMentions(ctx, text)
		}
		r.Text = text
		out = append(out, r.Reply)
	}
	return out
}

// SendReply implements model.Backend.
func (b *Backend) SendReply(m *model.Message, text string) {
	ctx := b.ctx
	cli := b.connected()
	jid, err := types.ParseJID(m.ChatID)
	if cli == nil || err != nil || strings.TrimSpace(text) == "" {
		return
	}
	msg := &waE2E.Message{Conversation: proto.String(text),
		MessageContextInfo: &waE2E.MessageContextInfo{MessageSecret: messageSecret()}}
	c := comment{id: string(cli.GenerateMessageID()), who: meVoter, at: b.sendTime(), receipt: model.Pending, payload: marshal(msg)}
	if err := b.store.putComment(ctx, b.db, m.ChatID, m.ID, c); err != nil {
		b.log.Errorf("store reply to %s in %s: %v", m.ID, m.ChatID, err)
		return
	}
	b.emitMessage(m.ChatID, m.ID)
	go func() {
		err := b.sendComment(ctx, cli, jid, m, msg, c.id)
		receipt := model.Sent
		if err != nil {
			receipt = model.Failed
			b.log.Warnf("reply to %s in %s: %v", m.ID, m.ChatID, err)
			text := "Couldn't send the reply."
			if errors.Is(err, whatsmeow.ErrOriginalMessageSecretNotFound) {
				text = "Couldn't send the reply: this announcement's key never came to this computer. Reply from your phone."
			}
			b.emit(model.NoticeEvent{Text: text})
		}
		b.store.setCommentReceipt(ctx, m.ChatID, c.id, receipt)
		b.emitMessage(m.ChatID, m.ID)
	}()
}

// sendComment encrypts msg as a reply to the announcement m and sends it.
func (b *Backend) sendComment(ctx context.Context, cli *whatsmeow.Client, chat types.JID, m *model.Message, msg *waE2E.Message, id string) error {
	root := b.senderOf(m)
	info := &types.MessageInfo{ID: m.ID, MessageSource: types.MessageSource{Chat: chat, Sender: root, IsFromMe: m.FromMe, IsGroup: true}}
	enc, err := cli.EncryptComment(ctx, info, msg)
	if err != nil {
		return err
	}
	// The stanza says which announcement it's a thread of, by LID.
	lid := root.ToNonAD()
	if lid.Server == types.DefaultUserServer {
		if j, err := cli.Store.LIDs.GetLIDForPN(ctx, lid); err == nil && !j.IsEmpty() {
			lid = j.ToNonAD()
		}
	}
	_, err = b.sendMessage(ctx, cli, chat, enc, whatsmeow.SendRequestExtra{ID: id,
		Meta: &types.MsgMetaInfo{ThreadMessageID: m.ID, ThreadMessageSenderJID: lid, DeprecatedLIDSession: proto.Bool(false)}})
	return err
}

// DeleteReply implements model.Backend.
func (b *Backend) DeleteReply(m *model.Message, r model.Reply) {
	ctx := b.ctx
	if r.Me && r.Receipt == model.Failed {
		b.store.dropComment(ctx, m.ChatID, r.ID, false, time.Time{}, "") // nobody else has it
		b.emitMessage(m.ChatID, m.ID)
		return
	}
	cli := b.connected()
	jid, err := types.ParseJID(m.ChatID)
	if cli == nil || err != nil {
		return
	}
	sender, by := types.EmptyJID, ""
	if !r.Me {
		if sender, err = types.ParseJID(r.SenderID); err != nil { // as an admin
			return
		}
		by = b.ownJID(m.ChatID).String()
	}
	keep := b.Pref(model.PrefKeepDeleted) == "on"
	b.store.dropComment(ctx, m.ChatID, r.ID, keep, b.now(), by)
	b.emitMessage(m.ChatID, m.ID)
	go func() {
		if _, err := b.sendMessage(ctx, cli, jid, cli.BuildRevoke(jid, sender, r.ID)); err != nil {
			b.log.Warnf("delete reply %s in %s: %v", r.ID, m.ChatID, err)
			b.emit(model.NoticeEvent{Text: "Couldn't delete the reply."})
			if keep { // it's still there, and can be deleted again
				b.store.unmarkComment(ctx, m.ChatID, r.ID)
				b.emitMessage(m.ChatID, m.ID)
			}
		}
	}()
}
