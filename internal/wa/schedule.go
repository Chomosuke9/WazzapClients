package wa

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	waBinary "github.com/polymorfa/hypermeow/binary"
	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	whatsmeow "github.com/polymorfa/hypermeow"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// WhatsApp's scheduled messages. The message goes to everyone when it's
// scheduled, as a conditionalRevealMessage: the real message, encrypted
// with AES-256-GCM under a random 32-byte reveal key, and the key's ID.
// The key goes to the server alone, in the stanza's
// <meta type="scheduled_message" st="<unix s>"><key rkid="<ID>">key</key></meta>,
// and the server gives it out at that time; until then nobody can read
// the message, nor knows it's there. Taking it back is a protocolMessage
// MESSAGE_UNSCHEDULE naming it, sent with edit="7" and no meta. A file is
// uploaded first, and its media message sealed the same way.
// WhatsApp takes times from 10 minutes (to the minute) to 14 days ahead,
// and holds up to 30 per chat; it answers more with error 419.
//
// Each is kept in wz_scheduled until it shows. Ours, and the ones your
// phone schedules (which come here as conditionalRevealMessages of your
// own without their key), are the chat's scheduled messages; at their
// time, the key decrypts them and they join the chat as messages
// (revealDue). Your phone's linked devices get its key with the message:
// the stanza carries the same meta the phone sent. Others' wait for their
// key, which the server hands out at the time as a mex notification from
// the sender (NotificationScheduledMessageReveal: chat_jid, rk in base64,
// rkid, sender_user_jid, st); the sender gets
// NotificationScheduledMessagePost (rkid, status SUCCESS or error_code)
// then. A message's key also comes in its history sync record
// (scheduledMessageMetadata).

const scheduleSchema = `CREATE TABLE IF NOT EXISTS wz_scheduled (
	chat        TEXT NOT NULL,
	id          TEXT NOT NULL,
	from_me     INTEGER NOT NULL DEFAULT 0,
	sender_jid  TEXT NOT NULL DEFAULT '',
	sender_push TEXT NOT NULL DEFAULT '',
	at          INTEGER NOT NULL DEFAULT 0, -- unix s it shows at, 0 = not known
	rkid        TEXT NOT NULL DEFAULT '',   -- its reveal key's ID
	payload     BLOB,                       -- the conditionalRevealMessage
	-- Ours: the schedDraft JSON it was made of; '' for others' and for
	-- the ones scheduled on another device.
	draft       TEXT NOT NULL DEFAULT '',
	-- 1 once the server took it; ours are 0 while they go.
	sent        INTEGER NOT NULL DEFAULT 1,
	created     INTEGER NOT NULL DEFAULT 0, -- unix s it came or was made here
	PRIMARY KEY (chat, id)
);
CREATE TABLE IF NOT EXISTS wz_reveal_keys (
	rkid TEXT PRIMARY KEY,
	key  BLOB NOT NULL,
	at   INTEGER NOT NULL DEFAULT 0, -- unix s, when the key said it
	-- Ours: the message it opens, which a failed post marks failed once
	-- it showed (scheduledFailed).
	chat TEXT NOT NULL DEFAULT '',
	id   TEXT NOT NULL DEFAULT ''
) WITHOUT ROWID;`

// schedDraft is a model.Draft as wz_scheduled keeps it.
type schedDraft struct {
	Text          string   `json:"text"`
	Shown         string   `json:"shown,omitempty"`
	Mentions      []string `json:"mentions,omitempty"`
	MentionAll    bool     `json:"all,omitempty"`
	MentionAdmins bool     `json:"admins,omitempty"`
	MentionChat   string   `json:"named,omitempty"`
	GroupNames    []string `json:"names,omitempty"`
	Reply         string   `json:"reply,omitempty"`
	// ReplyWhole is set when it replies to Reply's whole album.
	ReplyWhole *model.AlbumSize `json:"replyWhole,omitempty"`
	ViewOnce   bool             `json:"viewOnce,omitempty"`
	// A file's: how it's sent (model.Media), a document's name and a
	// photo's or video's thumbnail.
	Media int    `json:"media,omitempty"`
	File  string `json:"file,omitempty"`
	Thumb []byte `json:"thumb,omitempty"`
}

const (
	revealKeyLen = 32
	revealIVLen  = 12
	// keepScheduled is how long a scheduled message that never showed is
	// kept: WhatsApp keeps reveal keys 30 days.
	keepScheduled = 30 * 24 * time.Hour
	// schedMetaType is the type of the stanza's meta.
	schedMetaType = "scheduled_message"
)

// ErrScheduleTime is what Schedule says about a time WhatsApp won't take.
var ErrScheduleTime = errors.New("pick a time from 10 minutes to 14 days from now")

// scheduleWindow reports whether WhatsApp takes at as a scheduled
// message's time, now being now: at least ScheduleMin past the current
// minute, and at most ScheduleMax ahead.
func scheduleWindow(at, now time.Time) bool {
	return at.Sub(now.Truncate(time.Minute)) >= model.ScheduleMin && at.Sub(now) <= model.ScheduleMax
}

// Schedule implements model.Backend.
func (b *Backend) Schedule(chatID string, d model.Draft, shown string, at time.Time) error {
	jid, cli, at, err := b.scheduleCheck(chatID, at)
	if err != nil {
		return err
	}
	m := &model.Message{ID: cli.GenerateMessageID(), ChatID: chatID, FromMe: true, Text: d.Text}
	msg, prep := b.textMessage(cli, jid, d, m)
	return b.schedule(cli, jid, m.ID, newSchedDraft(d, shown), at, func(ctx context.Context) (*waE2E.Message, error) {
		if prep != nil {
			prep(ctx)
		}
		return msg, nil
	})
}

// ScheduleFile implements model.Backend. The file is uploaded as the
// message goes to the others, sealed; at its time they download it like
// any other.
func (b *Backend) ScheduleFile(chatID string, a model.Attachment, d model.Draft, shown string, at time.Time) error {
	jid, cli, at, err := b.scheduleCheck(chatID, at)
	if err != nil {
		return err
	}
	a.Album = "" // each goes alone: an album's pictures point at its message
	m, up, err := readFile(a, d.Text)
	if err != nil {
		return err
	}
	m.ID, m.ChatID = cli.GenerateMessageID(), chatID
	ci := b.draftContext(chatID, d, m)
	msg, inner := fileMessage(m, up, d.Text, ci, a.ViewOnce)
	b.keepSent(m, up, msg) // so it shows without a download once it's here
	sd := newSchedDraft(d, shown)
	sd.Media, sd.File, sd.Thumb, sd.ViewOnce = int(m.Media), m.FileName, m.Thumb, a.ViewOnce
	if m.Media != model.MediaDocument {
		sd.File = ""
	}
	return b.schedule(cli, jid, m.ID, sd, at, func(ctx context.Context) (*waE2E.Message, error) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
		res, err := up.push(ctx, cli, m.Media, false)
		if err != nil {
			return nil, err
		}
		uploaded(inner, res)
		return msg, nil
	})
}

// scheduleCheck checks that chatID can take a message scheduled at at,
// and returns at to the minute.
func (b *Backend) scheduleCheck(chatID string, at time.Time) (types.JID, *whatsmeow.Client, time.Time, error) {
	jid, err := types.ParseJID(chatID)
	cli := b.client()
	if err != nil || cli == nil {
		return jid, nil, at, errors.New("this chat can't take scheduled messages")
	}
	if jid.Server == types.NewsletterServer || jid.Server == types.BroadcastServer {
		return jid, nil, at, errors.New("channels and broadcasts can't take scheduled messages")
	}
	at = at.Truncate(time.Minute)
	if !scheduleWindow(at, b.sendTime()) {
		return jid, nil, at, ErrScheduleTime
	}
	if n := len(b.Scheduled(chatID)); n >= model.ScheduleMaxPerChat {
		return jid, nil, at, fmt.Errorf("WhatsApp holds up to %d scheduled messages per chat", model.ScheduleMaxPerChat)
	}
	return jid, cli, at, nil
}

func newSchedDraft(d model.Draft, shown string) schedDraft {
	sd := schedDraft{Text: d.Text, Shown: shown, Mentions: d.Mentions, MentionAll: d.MentionAll,
		MentionAdmins: d.MentionAdmins, MentionChat: d.MentionChat, GroupNames: d.GroupNames, ViewOnce: d.ViewOnce}
	if d.Reply != nil {
		sd.Reply, sd.ReplyWhole = d.Reply.ID, d.Reply.Whole
	}
	return sd
}

// schedule lists the message id, sd, as scheduled at at, under a new
// reveal key, and sends what build makes (in the background: an upload
// may take a while) sealed under it. One that fails leaves the list,
// with a notice.
func (b *Backend) schedule(cli *whatsmeow.Client, jid types.JID, id string, sd schedDraft, at time.Time,
	build func(context.Context) (*waE2E.Message, error),
) error {
	chatID := jid.String()
	draft, _ := json.Marshal(sd)
	key := make([]byte, revealKeyLen)
	rkidRaw := make([]byte, 16)
	_, _ = rand.Read(key)
	_, _ = rand.Read(rkidRaw)
	rkid := strings.ToUpper(hex.EncodeToString(rkidRaw)) // as phones make them
	ctx := b.ctx
	if _, err := b.db.ExecContext(ctx, `INSERT INTO wz_scheduled (chat, id, from_me, at, rkid, draft, sent, created)
		VALUES (?, ?, 1, ?, ?, ?, 0, ?)`, chatID, id, at.Unix(), rkid, string(draft), b.now().Unix()); err != nil {
		return err
	}
	err := b.putRevealKey(ctx, rkid, key, at.Unix())
	if err == nil {
		_, err = b.db.ExecContext(ctx, `UPDATE wz_reveal_keys SET chat = ?, id = ? WHERE rkid = ?`, chatID, id, rkid)
	}
	if err != nil {
		_, _ = b.db.ExecContext(ctx, `DELETE FROM wz_scheduled WHERE chat = ? AND id = ?`, chatID, id)
		return err
	}
	b.emit(model.ScheduledEvent{ChatID: chatID})
	b.goWork(func() {
		msg, err := build(ctx)
		if err == nil {
			err = b.sendScheduled(ctx, cli, jid, id, msg, key, rkid, at)
		}
		if err != nil {
			b.log.Errorf("schedule a message in %s: %v", chatID, err)
			_, _ = b.db.ExecContext(ctx, `DELETE FROM wz_scheduled WHERE chat = ? AND id = ?`, chatID, id)
			b.emit(model.ScheduledEvent{ChatID: chatID})
			b.emit(model.NoticeEvent{Text: scheduleFailed(err)})
			return
		}
		b.kickSchedule()
	})
	return nil
}

// scheduleFailed words why a message couldn't be scheduled.
func scheduleFailed(err error) string {
	if strings.Contains(err.Error(), "419") { // WhatsApp's "too many"
		return fmt.Sprintf("Couldn't schedule the message: WhatsApp holds up to %d per chat.", model.ScheduleMaxPerChat)
	}
	return "Couldn't schedule the message."
}

// sendScheduled encrypts msg under key and sends it as a scheduled message
// with ID id, which the server shows at at.
func (b *Backend) sendScheduled(ctx context.Context, cli *whatsmeow.Client, jid types.JID, id string,
	msg *waE2E.Message, key []byte, rkid string, at time.Time,
) error {
	plain, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	iv := make([]byte, revealIVLen)
	_, _ = rand.Read(iv)
	enc, err := revealSeal(key, iv, plain)
	if err != nil {
		return err
	}
	outer := &waE2E.Message{ConditionalRevealMessage: &waE2E.ConditionalRevealMessage{
		EncPayload:                   enc,
		EncIV:                        iv,
		ConditionalRevealMessageType: waE2E.ConditionalRevealMessage_SCHEDULED_MESSAGE.Enum(),
		RevealKeyID:                  proto.String(rkid),
	}}
	if _, err := b.db.ExecContext(ctx, `UPDATE wz_scheduled SET payload = ? WHERE chat = ? AND id = ?`,
		marshal(outer), jid.String(), id); err != nil {
		return err
	}
	meta := []waBinary.Node{schedMeta(rkid, key, at)}
	if _, err := b.sendMessage(ctx, cli, jid, outer, whatsmeow.SendRequestExtra{ID: id, AdditionalNodes: &meta}); err != nil {
		return err
	}
	_, err = b.db.ExecContext(ctx, `UPDATE wz_scheduled SET sent = 1 WHERE chat = ? AND id = ?`, jid.String(), id)
	return err
}

// schedMeta is the stanza's meta that hands the server a scheduled
// message's key and time.
func schedMeta(rkid string, key []byte, at time.Time) waBinary.Node {
	return waBinary.Node{Tag: "meta",
		Attrs:   waBinary.Attrs{"type": schedMetaType, "st": strconv.FormatInt(at.Unix(), 10)},
		Content: []waBinary.Node{{Tag: "key", Attrs: waBinary.Attrs{"rkid": rkid}, Content: key}}}
}

func revealSeal(key, iv, plain []byte) ([]byte, error) {
	g, err := revealGCM(key)
	if err != nil {
		return nil, err
	}
	return g.Seal(nil, iv, plain, nil), nil
}

func revealOpen(key, iv, enc []byte) ([]byte, error) {
	g, err := revealGCM(key)
	if err != nil {
		return nil, err
	}
	if len(iv) != revealIVLen {
		return nil, errors.New("bad reveal IV")
	}
	return g.Open(nil, iv, enc, nil)
}

func revealGCM(key []byte) (cipher.AEAD, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(blk)
}

// Scheduled implements model.Backend.
func (b *Backend) Scheduled(chatID string) []*model.Scheduled {
	q := `SELECT s.chat, s.id, s.at, s.draft, s.payload, k.key FROM wz_scheduled s
		LEFT JOIN wz_reveal_keys k ON k.rkid = s.rkid
		WHERE s.from_me = 1 AND s.at > ?`
	args := []any{b.now().Add(-time.Minute).Unix()}
	if chatID != "" {
		q += ` AND s.chat = ?`
		args = append(args, chatID)
	}
	rows, err := b.db.QueryContext(b.ctx, q+` ORDER BY s.at, s.rowid`, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []*model.Scheduled
	for rows.Next() {
		var s model.Scheduled
		var at int64
		var draft string
		var payload, key []byte
		if rows.Scan(&s.ChatID, &s.ID, &at, &draft, &payload, &key) != nil {
			continue
		}
		s.At = time.Unix(at, 0)
		var sd schedDraft
		if draft != "" && json.Unmarshal([]byte(draft), &sd) == nil {
			s.Draft = model.Draft{Text: sd.Text, Mentions: sd.Mentions, MentionAll: sd.MentionAll,
				MentionAdmins: sd.MentionAdmins, MentionChat: sd.MentionChat, GroupNames: sd.GroupNames,
				ViewOnce: sd.ViewOnce}
			if sd.Reply != "" {
				s.Draft.Reply = &model.Message{ID: sd.Reply, ChatID: s.ChatID, Whole: sd.ReplyWhole}
			}
			s.Shown = sd.Shown
			s.Media, s.FileName, s.Thumb = model.Media(sd.Media), sd.File, sd.Thumb
		} else {
			// Scheduled on your phone: what it says shows when its key is
			// here (its history sync record has it).
			s.Elsewhere = true
			if inner := openScheduled(payload, key); inner != nil {
				c := describe(unwrap(inner))
				s.Shown = namedGroupMentions(c.text, "")
				s.Draft.ViewOnce = isViewOnce(inner)
				s.Media, s.Thumb = c.media, c.thumb
				if c.media == model.MediaDocument {
					s.FileName = c.file.Name
				}
			}
		}
		out = append(out, &s)
	}
	return out
}

// openScheduled decrypts a stored conditionalRevealMessage with key, or
// returns nil.
func openScheduled(payload, key []byte) *waE2E.Message {
	if len(payload) == 0 || len(key) == 0 {
		return nil
	}
	var outer waE2E.Message
	if proto.Unmarshal(payload, &outer) != nil {
		return nil
	}
	cr := outer.GetConditionalRevealMessage()
	plain, err := revealOpen(key, cr.GetEncIV(), cr.GetEncPayload())
	if err != nil {
		return nil
	}
	var inner waE2E.Message
	if proto.Unmarshal(plain, &inner) != nil {
		return nil
	}
	return &inner
}

// Unschedule implements model.Backend. One the server took is taken back
// in the background, and stays on the list until the server says it's
// gone: a NoticeEvent says how that went. Phones send the
// MESSAGE_UNSCHEDULE as they send a delete for everyone (edit="7", which
// hypermeow's patch gives it), with no meta.
func (b *Backend) Unschedule(chatID, id string) bool {
	ctx := b.ctx
	var sent bool
	err := b.db.QueryRowContext(ctx, `SELECT sent FROM wz_scheduled WHERE chat = ? AND id = ? AND from_me = 1 AND at > ?`,
		chatID, id, b.now().Unix()).Scan(&sent)
	if err != nil {
		return false
	}
	forget := func() {
		_, _ = b.db.ExecContext(ctx, `DELETE FROM wz_scheduled WHERE chat = ? AND id = ?`, chatID, id)
		b.emit(model.ScheduledEvent{ChatID: chatID})
	}
	jid, err := types.ParseJID(chatID)
	cli := b.client()
	if !sent {
		forget() // it never went
		return true
	}
	if err != nil || cli == nil {
		return false
	}
	msg := &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Key:  &waCommon.MessageKey{RemoteJID: proto.String(chatID), FromMe: proto.Bool(true), ID: proto.String(id)},
		Type: waE2E.ProtocolMessage_MESSAGE_UNSCHEDULE.Enum(),
	}}
	b.goWork(func() {
		if _, err := b.sendMessage(ctx, cli, jid, msg); err != nil {
			b.log.Errorf("unschedule %s in %s: %v", id, chatID, err)
			b.emit(model.NoticeEvent{Text: "Couldn't cancel the scheduled message: it will still be sent."})
			return
		}
		forget()
		b.emit(model.NoticeEvent{Text: "Scheduled message cancelled"})
	})
	return true
}

// unscheduled forgets a message its sender took back before its time
// (MESSAGE_UNSCHEDULE), from your phone or someone else.
func (b *Backend) unscheduled(ctx context.Context, chat, id string) {
	if id == "" {
		return
	}
	r, err := b.db.ExecContext(ctx, `DELETE FROM wz_scheduled WHERE chat = ? AND id = ?`, chat, id)
	if err != nil {
		return
	}
	n, _ := r.RowsAffected()
	b.log.Infof("scheduled message %s in %s taken back (%d kept)", id, chat, n)
	if n > 0 {
		b.emit(model.ScheduledEvent{ChatID: chat})
	}
}

// parseScheduled handles a conditionalRevealMessage that came in: the
// message it holds, when its key is here and its time has come; otherwise
// it's kept (wz_scheduled) until then, and nothing shows.
func (b *Backend) parseScheduled(ctx context.Context, evt *events.Message, chat types.JID, cr *waE2E.ConditionalRevealMessage) (parsed, bool) {
	rkid := cr.GetRevealKeyID()
	if md := evt.SourceWebMsg.GetScheduledMessageMetadata(); md != nil && len(md.GetRevealKey()) == revealKeyLen {
		// History sync carries the key of your own.
		if md.GetRevealKeyID() != "" {
			rkid = md.GetRevealKeyID()
		}
		_ = b.putRevealKey(ctx, rkid, md.GetRevealKey(), unixSeconds(int64(md.GetScheduledTime())))
	}
	if cr.GetConditionalRevealMessageType() != waE2E.ConditionalRevealMessage_SCHEDULED_MESSAGE || rkid == "" {
		b.log.Infof("conditional reveal message %s in %s of type %s: not shown", evt.Info.ID, chat, cr.GetConditionalRevealMessageType())
		return parsed{}, false
	}
	payload := marshal(&waE2E.Message{ConditionalRevealMessage: cr})
	key, keyAt := b.revealKey(ctx, rkid)
	// Rows stored by an earlier version say when, when the stanza did.
	_, err := b.db.ExecContext(ctx, `INSERT INTO wz_scheduled (chat, id, from_me, sender_jid, sender_push, at, rkid, payload, created)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (chat, id) DO UPDATE SET payload = COALESCE(wz_scheduled.payload, excluded.payload),
			at = MAX(wz_scheduled.at, excluded.at)`,
		chat.String(), evt.Info.ID, boolInt(evt.Info.IsFromMe), evt.Info.Sender.ToNonAD().String(), evt.Info.PushName,
		keyAt, rkid, payload, b.now().Unix())
	if err != nil {
		b.log.Warnf("store scheduled message %s: %v", evt.Info.ID, err)
		return parsed{}, false
	}
	b.log.Infof("scheduled message %s in %s (key %s): key here %v, at %d", evt.Info.ID, chat, rkid, key != nil, keyAt)
	if evt.Info.IsFromMe {
		b.emit(model.ScheduledEvent{ChatID: chat.String()})
	}
	b.kickSchedule() // it may be due already
	return parsed{}, false
}

// putRevealKey keeps a reveal key, and when the server says it shows.
func (b *Backend) putRevealKey(ctx context.Context, rkid string, key []byte, at int64) error {
	if rkid == "" || len(key) != revealKeyLen {
		return nil
	}
	_, err := b.db.ExecContext(ctx, `INSERT INTO wz_reveal_keys (rkid, key, at) VALUES (?, ?, ?)
		ON CONFLICT (rkid) DO UPDATE SET key = excluded.key, at = CASE WHEN excluded.at > 0 THEN excluded.at ELSE wz_reveal_keys.at END`,
		rkid, key, at)
	if err == nil && at > 0 {
		_, err = b.db.ExecContext(ctx, `UPDATE wz_scheduled SET at = ? WHERE rkid = ? AND at = 0`, at, rkid)
	}
	return err
}

// revealKey returns the reveal key with ID rkid and its time, or nil.
func (b *Backend) revealKey(ctx context.Context, rkid string) (key []byte, at int64) {
	_ = b.db.QueryRowContext(ctx, `SELECT key, at FROM wz_reveal_keys WHERE rkid = ?`, rkid).Scan(&key, &at)
	return key, at
}

// kickSchedule has runScheduled look at the scheduled messages again.
func (b *Backend) kickSchedule() {
	select {
	case b.schedKick <- struct{}{}:
	default:
	}
}

// runScheduled shows the scheduled messages whose time came, as it comes.
// It looks again at least hourly, in case the clock jumped (sleep).
func (b *Backend) runScheduled() {
	b.tidyScheduled(b.ctx)
	for {
		next := b.revealDue(b.ctx)
		wait := time.Hour
		if !next.IsZero() {
			wait = min(max(next.Sub(b.now()), 0), time.Hour)
		}
		t := time.NewTimer(wait)
		select {
		case <-b.ctx.Done():
			t.Stop()
			return
		case <-t.C:
		case <-b.schedKick:
			t.Stop()
		}
	}
}

// tidyScheduled drops what won't show: ours that never went out (the app
// closed while they did) and anything past WhatsApp's 30 days. It also
// moves the scheduled messages an earlier version stored as messages it
// couldn't show into wz_scheduled, once.
func (b *Backend) tidyScheduled(ctx context.Context) {
	_, _ = b.db.ExecContext(ctx, `DELETE FROM wz_scheduled WHERE from_me = 1 AND sent = 0`)
	old := b.now().Add(-keepScheduled).Unix()
	_, _ = b.db.ExecContext(ctx, `DELETE FROM wz_scheduled WHERE CASE WHEN at != 0 THEN at ELSE created END < ?`, old)
	_, _ = b.db.ExecContext(ctx, `DELETE FROM wz_reveal_keys WHERE at != 0 AND at < ?`, old)
	const done = "scheduled_moved"
	if b.store.meta(ctx, done) != "" {
		return
	}
	rows, err := b.db.QueryContext(ctx, `SELECT chat, id, from_me, sender_jid, sender_push, raw_payload
		FROM wz_messages WHERE kind = ?`, int(model.KindUnsupported))
	if err != nil {
		return
	}
	type row struct {
		chat, id, sender, push string
		fromMe                 bool
		cr                     *waE2E.ConditionalRevealMessage
	}
	var found []row
	for rows.Next() {
		var r row
		var raw []byte
		if rows.Scan(&r.chat, &r.id, &r.fromMe, &r.sender, &r.push, &raw) != nil {
			continue
		}
		var m waE2E.Message
		if proto.Unmarshal(raw, &m) != nil {
			continue
		}
		if r.cr = unwrap(&m).GetConditionalRevealMessage(); r.cr != nil {
			found = append(found, r)
		}
	}
	rows.Close()
	for _, r := range found {
		_, err := b.db.ExecContext(ctx, `INSERT OR IGNORE INTO wz_scheduled (chat, id, from_me, sender_jid, sender_push, rkid, payload, created)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, r.chat, r.id, boolInt(r.fromMe), r.sender, r.push, r.cr.GetRevealKeyID(),
			marshal(&waE2E.Message{ConditionalRevealMessage: r.cr}), b.now().Unix())
		if err == nil {
			_, _ = b.db.ExecContext(ctx, `DELETE FROM wz_messages WHERE chat = ? AND id = ?`, r.chat, r.id)
			b.emit(model.DeletedEvent{ChatID: r.chat, IDs: []string{r.id}})
			b.emitChat(r.chat)
		}
	}
	_ = b.store.setMetaValue(ctx, done, "1")
}

// revealDue shows the scheduled messages whose key is here and whose time
// came, and returns when the next one is due (zero for none known).
func (b *Backend) revealDue(ctx context.Context) time.Time {
	rows, err := b.db.QueryContext(ctx, `SELECT s.chat, s.id, s.from_me, s.sender_jid, s.sender_push, s.at, s.payload, k.key
		FROM wz_scheduled s JOIN wz_reveal_keys k ON k.rkid = s.rkid WHERE s.sent = 1 AND s.payload IS NOT NULL`)
	if err != nil {
		return time.Time{}
	}
	type due struct {
		chat, id, sender, push string
		fromMe                 bool
		at                     int64
		payload, key           []byte
	}
	var list []due
	var next time.Time
	now := b.now()
	for rows.Next() {
		var d due
		if rows.Scan(&d.chat, &d.id, &d.fromMe, &d.sender, &d.push, &d.at, &d.payload, &d.key) != nil {
			continue
		}
		at := time.Unix(d.at, 0)
		if d.at != 0 && at.After(now) {
			if next.IsZero() || at.Before(next) {
				next = at
			}
			continue
		}
		list = append(list, d)
	}
	rows.Close()
	for _, d := range list {
		b.reveal(ctx, d.chat, d.id, d.fromMe, d.sender, d.push, d.at, d.payload, d.key)
	}
	return next
}

// reveal shows a scheduled message whose time came: the message it holds
// joins its chat, as if it came then.
func (b *Backend) reveal(ctx context.Context, chat, id string, fromMe bool, sender, push string, at int64, payload, key []byte) {
	_, _ = b.db.ExecContext(ctx, `DELETE FROM wz_scheduled WHERE chat = ? AND id = ?`, chat, id)
	defer b.emit(model.ScheduledEvent{ChatID: chat})
	inner := openScheduled(payload, key)
	if inner == nil {
		b.log.Warnf("scheduled message %s in %s: its key doesn't open it", id, chat)
		return
	}
	cj, err := types.ParseJID(chat)
	if err != nil {
		return
	}
	when := b.now()
	if at != 0 {
		when = time.Unix(at, 0)
	}
	info := types.MessageInfo{ID: id, Timestamp: when, PushName: push}
	info.Chat, info.IsFromMe, info.IsGroup = cj, fromMe, cj.Server == types.GroupServer
	if s, err := types.ParseJID(sender); err == nil {
		info.Sender = s
	} else if fromMe {
		info.Sender = b.ownJID(chat)
	}
	evt := &events.Message{Info: info, Message: unwrap(inner), RawMessage: inner}
	p, ok := b.parse(ctx, evt)
	if !ok {
		return
	}
	b.apply(&evt.Info, p)
}

// rawNodes reads what WhatsApp's server says about scheduled messages
// before hypermeow handles a node, then passes it on to groupedReceipts.
func (b *Backend) rawNodes(ctx context.Context, node *waBinary.Node) (*waBinary.Node, bool) {
	if node.Tag == "notification" && node.Attrs["type"] == "mex" {
		b.noteScheduledMex(ctx, node)
	}
	b.noteRevealKeys(ctx, node)
	b.noteOffline(node)
	return b.groupedReceipts(ctx, node)
}

// schedMex is the JSON of the mex notifications about scheduled messages.
type schedMex struct {
	Data struct {
		Reveal *struct {
			Chat   string `json:"chat_jid"`
			Key    []byte `json:"rk"` // base64
			RKID   string `json:"rkid"`
			Sender string `json:"sender_user_jid"`
			ST     int64  `json:"st"`
		} `json:"xwa2_notify_scheduled_message_reveal"`
		Post *struct {
			Chat   string `json:"chat_jid"`
			Error  any    `json:"error_code"`
			RKID   string `json:"rkid"`
			Status string `json:"status"`
		} `json:"xwa2_notify_scheduled_message_post"`
	} `json:"data"`
}

// noteScheduledMex reads a mex notification about a scheduled message:
// someone's message's key, at its time, or whether one of yours went.
func (b *Backend) noteScheduledMex(ctx context.Context, node *waBinary.Node) {
	for _, u := range node.GetChildrenByTag("update") {
		op, _ := u.Attrs["op_name"].(string)
		data, ok := u.Content.([]byte)
		if !ok || !strings.Contains(op, "ScheduledMessage") {
			continue
		}
		var m schedMex
		if err := json.Unmarshal(data, &m); err != nil {
			b.log.Warnf("%s: %v", op, err)
			continue
		}
		switch r, p := m.Data.Reveal, m.Data.Post; {
		case r != nil:
			b.log.Infof("%s: key %s of %s in %s, at %d (%d bytes)", op, r.RKID, r.Sender, r.Chat, r.ST, len(r.Key))
			if err := b.putRevealKey(ctx, r.RKID, r.Key, r.ST); err != nil {
				b.log.Warnf("keep reveal key %s: %v", r.RKID, err)
			}
			b.kickSchedule()
		case p != nil:
			b.log.Infof("%s: %s in %s: %s %v", op, p.RKID, p.Chat, p.Status, p.Error)
			if p.Status != "SUCCESS" {
				b.scheduledFailed(ctx, p.RKID)
			}
		default:
			b.log.Infof("scheduled message mex %s: %s", op, data)
		}
	}
}

// scheduledFailed forgets one of your scheduled messages that WhatsApp
// says didn't go, and says so.
func (b *Backend) scheduledFailed(ctx context.Context, rkid string) {
	var chat, id string
	if b.db.QueryRowContext(ctx, `SELECT chat, id FROM wz_reveal_keys WHERE rkid = ? AND id != ''`, rkid).Scan(&chat, &id) != nil {
		return // not one sent from here
	}
	_, _ = b.db.ExecContext(ctx, `DELETE FROM wz_scheduled WHERE chat = ? AND id = ?`, chat, id)
	// It showed as sent at its time, moments before the server said.
	_, _ = b.db.ExecContext(ctx, `UPDATE wz_messages SET receipt = ? WHERE chat = ? AND id = ? AND receipt <= ?`,
		int(model.Failed), chat, id, int(model.Sent))
	b.emit(model.ScheduledEvent{ChatID: chat})
	b.emit(model.ReceiptEvent{ChatID: chat, IDs: []string{id}, Receipt: model.Failed})
	b.emit(model.NoticeEvent{Text: "WhatsApp couldn't send a scheduled message."})
}

// noteRevealKeys keeps the reveal keys a node carries, in a
// <meta type="scheduled_message"> or anywhere else a <key rkid> sits, and
// logs where it found them: how the server hands recipients their keys
// isn't known yet.
func (b *Backend) noteRevealKeys(ctx context.Context, node *waBinary.Node) {
	found := false
	var walk func(n *waBinary.Node, st int64, depth int)
	walk = func(n *waBinary.Node, st int64, depth int) {
		if depth > 4 {
			return
		}
		if n.Tag == "meta" {
			if t, _ := n.Attrs["type"].(string); t == schedMetaType {
				if s, ok := n.Attrs["st"]; ok {
					st, _ = strconv.ParseInt(fmt.Sprint(s), 10, 64)
				}
			}
		}
		if rkid, ok := n.Attrs["rkid"].(string); ok && rkid != "" {
			found = true
			key, _ := n.Content.([]byte)
			b.log.Infof("reveal key %s in a <%s> of %s (%s): %d bytes, at %d", rkid, n.Tag, node.Tag, nodeAttrs(node), len(key), st)
			if len(key) == revealKeyLen {
				if err := b.putRevealKey(ctx, rkid, key, st); err != nil {
					b.log.Warnf("keep reveal key %s: %v", rkid, err)
				}
			}
		}
		for i := range n.GetChildren() {
			walk(&n.GetChildren()[i], st, depth+1)
		}
	}
	walk(node, 0, 0)
	if found {
		b.kickSchedule()
		return
	}
	if node.Tag == "notification" || node.Tag == "ib" {
		if t := fmt.Sprint(node.Attrs["type"]); strings.Contains(t, "sched") || strings.Contains(t, "reveal") {
			b.log.Infof("scheduled message node: %s", node.String())
		}
	}
}

// nodeAttrs lists a node's attributes for the log, without its children.
func nodeAttrs(n *waBinary.Node) string {
	var sb strings.Builder
	for k, v := range n.Attrs {
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%s=%v", k, v)
	}
	return sb.String()
}
