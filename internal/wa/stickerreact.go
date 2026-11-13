package wa

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// A sticker reaction is a sticker message of its own whose
// messageContextInfo points to the message it reacts to (a
// STICKER_ANNOTATION association). WhatsApp shows it on that message's
// corner, not in the chat, so it's no message here either: parse files it
// under the message, in wz_sticker_reacts, as its payload. Taking it back
// is deleting the sticker message for everyone. A message shows each
// person's latest.
//
// The columns are named apart from wz_messages', so that stickerColumn can
// sit in any query of messages.
const stickerReactSchema = `
CREATE TABLE IF NOT EXISTS wz_sticker_reacts (
	rchat   TEXT NOT NULL,
	parent  TEXT NOT NULL,              -- the message reacted to
	sid     TEXT NOT NULL,              -- the sticker message's own ID
	who     TEXT NOT NULL,              -- usually a LID; meVoter for you
	rts     INTEGER NOT NULL,           -- unix milliseconds
	lottie  INTEGER NOT NULL DEFAULT 0, -- 1 for a Lottie sticker
	payload BLOB NOT NULL,              -- the waE2E message it came or went as
	PRIMARY KEY (rchat, sid)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS wz_sticker_reacts_parent ON wz_sticker_reacts (rchat, parent);
`

// stickerColumn is a message's sticker reactions, as readStickers takes
// them: a line each, tab separated.
const stickerColumn = `COALESCE((SELECT group_concat(sid || char(9) || who || char(9) || lottie || char(9) || rts, char(10))
	FROM wz_sticker_reacts WHERE rchat = chat AND parent = id), '')`

// stickerReact is a sticker someone reacted to p.target with.
type stickerReact struct {
	sid     string
	who     string
	ts      int64 // unix milliseconds
	lottie  bool
	payload []byte
}

// annotates returns the ID of the message a sticker message reacts to, or
// "". The association can sit on any of the envelopes around the sticker,
// as an album's can.
func annotates(m *waE2E.Message) string {
	for i := 0; i < 5 && m != nil; i++ {
		a := m.GetMessageContextInfo().GetMessageAssociation()
		if a.GetAssociationType() == waE2E.MessageAssociation_STICKER_ANNOTATION {
			if id := a.GetParentMessageKey().GetID(); id != "" {
				return id
			}
		}
		m = unwrapOnce(m)
	}
	return ""
}

// readStickers reads what stickerColumn holds: each person's latest
// sticker, oldest first. Names are resolve's to give.
func readStickers(s string) []model.StickerReaction {
	if s == "" {
		return nil
	}
	var out []model.StickerReaction
	for _, l := range strings.Split(s, "\n") {
		f := strings.Split(l, "\t")
		if len(f) != 4 {
			continue
		}
		ts, _ := strconv.ParseInt(f[3], 10, 64)
		r := model.StickerReaction{ID: f[0], SenderID: f[1], Me: f[1] == meVoter, Lottie: f[2] == "1", Time: time.UnixMilli(ts)}
		i := slices.IndexFunc(out, func(o model.StickerReaction) bool { return o.SenderID == r.SenderID })
		switch {
		case i < 0:
			out = append(out, r)
		case !r.Time.Before(out[i].Time):
			out[i] = r
		}
	}
	slices.SortStableFunc(out, func(a, b model.StickerReaction) int { return a.Time.Compare(b.Time) })
	return out
}

// nameStickers gives a message's sticker reactions their senders' names.
func (b *Backend) nameStickers(ctx context.Context, m *model.Message) {
	for i := range m.Stickers {
		r := &m.Stickers[i]
		if r.Me {
			r.SenderID, r.Sender = b.ownJID(m.ChatID).String(), "You"
		} else {
			r.Sender = b.senderNameStr(ctx, r.SenderID, "")
		}
	}
}

// putStickerReact stores a sticker reaction to the message parent, or the
// payload it went out as.
func (s *msgStore) putStickerReact(ctx context.Context, x execer, chat, parent string, r stickerReact) error {
	_, err := x.ExecContext(ctx, `INSERT INTO wz_sticker_reacts (rchat, parent, sid, who, rts, lottie, payload)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (rchat, sid) DO UPDATE SET payload = excluded.payload`,
		chat, parent, r.sid, r.who, r.ts, boolInt(r.lottie), r.payload)
	return err
}

// dropStickerReact removes the sticker reaction whose sticker message is
// sid and returns the message it reacted to, or "" when sid isn't one.
func (s *msgStore) dropStickerReact(ctx context.Context, chat, sid string) (parent string) {
	_ = s.db.QueryRowContext(ctx, `DELETE FROM wz_sticker_reacts WHERE rchat = ? AND sid = ? RETURNING parent`,
		chat, sid).Scan(&parent)
	return parent
}

// myStickerReacts lists the sticker messages you reacted to a message with.
func (s *msgStore) myStickerReacts(ctx context.Context, chat, parent string) (sids []string) {
	rows, err := s.db.QueryContext(ctx, `SELECT sid FROM wz_sticker_reacts WHERE rchat = ? AND parent = ? AND who = ?`,
		chat, parent, meVoter)
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var sid string
		if rows.Scan(&sid) == nil {
			sids = append(sids, sid)
		}
	}
	return sids
}

// stickerReactPayload returns the payload of the sticker reaction sid.
func (s *msgStore) stickerReactPayload(ctx context.Context, chat, sid string) (raw []byte, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT payload FROM wz_sticker_reacts WHERE rchat = ? AND sid = ?`, chat, sid).Scan(&raw)
	return
}

// foldStickerReacts files the sticker reactions an older version stored as
// messages of the chat under the messages they react to, once.
func (s *msgStore) foldStickerReacts(ctx context.Context) error {
	const done = "sticker_reacts_folded"
	if s.meta(ctx, done) != "" {
		return nil
	}
	type found struct {
		chat, parent string
		stickerReact
	}
	var all []found
	rows, err := s.db.QueryContext(ctx, `SELECT chat, id, sender_jid, from_me, ts, raw_payload FROM wz_messages
		WHERE media = ? AND raw_payload IS NOT NULL`, int(model.MediaSticker))
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			f      found
			fromMe bool
			ts     int64
		)
		if err := rows.Scan(&f.chat, &f.sid, &f.who, &fromMe, &ts, &f.payload); err != nil {
			rows.Close()
			return err
		}
		c, pm := describeRaw(f.payload)
		if pm == nil {
			continue
		}
		if f.parent = annotates(pm); f.parent == "" || f.parent == f.sid {
			continue
		}
		if fromMe {
			f.who = meVoter
		}
		f.ts, f.lottie = ts*1000, c.lottie
		all = append(all, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, f := range all {
		if err := s.putStickerReact(ctx, tx, f.chat, f.parent, f.stickerReact); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM wz_messages WHERE chat = ?1 AND id = ?2`, f.chat, f.sid); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM wz_receipts WHERE chat = ?1 AND id = ?2`, f.chat, f.sid); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.setMetaValue(ctx, done, "1")
}

// stickerMessageOf returns the sticker a sticker message (of a chat, or
// of the picker) sends, without its context, and its file when that is
// still to upload: a sticker of a pack's zip. It reports false, with a
// notice, when the sticker can't be sent.
func (b *Backend) stickerMessageOf(sticker *model.Message) (e *waE2E.StickerMessage, data []byte, ok bool) {
	if sticker.ChatID == stickerChat && strings.HasPrefix(sticker.ID, encStickerPrefix) {
		b.emit(model.NoticeEvent{Text: "This sticker is still loading."})
		return nil, nil, false
	}
	if s := b.unuploaded(sticker.ID); sticker.ChatID == stickerChat && s != nil {
		data, _ = os.ReadFile(b.mediaPath(stickerChat, sticker.ID))
		if len(data) == 0 {
			b.emit(model.NoticeEvent{Text: "This sticker is still loading."})
			return nil, nil, false
		}
		return &waE2E.StickerMessage{Mimetype: s.Mimetype, Width: s.Width, Height: s.Height, IsAnimated: s.IsAnimated,
			IsLottie: s.IsLottie, AccessibilityLabel: s.AccessibilityLabel, Emojis: s.Emojis,
			FileLength: proto.Uint64(uint64(len(data)))}, data, true
	}
	media, blob, err := b.store.mediaBlob(b.ctx, sticker.ChatID, sticker.ID)
	e = &waE2E.StickerMessage{}
	if err != nil || media != model.MediaSticker || len(blob) == 0 || proto.Unmarshal(blob, e) != nil {
		return nil, nil, false
	}
	e.ContextInfo = nil
	return e, nil, true
}

// ReactSticker implements model.Backend.
func (b *Backend) ReactSticker(m *model.Message, sticker *model.Message) {
	cli := b.connected()
	jid, err := types.ParseJID(m.ChatID)
	if cli == nil || err != nil || isChannel(jid) {
		return
	}
	ctx, chat := b.ctx, m.ChatID
	old := b.store.myStickerReacts(ctx, chat, m.ID)
	takeBack := func() {
		for _, sid := range old {
			if _, err := b.sendMessage(ctx, cli, jid, cli.BuildRevoke(jid, types.EmptyJID, sid)); err != nil {
				b.log.Warnf("take a sticker reaction back in %s: %v", chat, err)
			}
		}
	}
	if sticker == nil {
		for _, sid := range old {
			b.store.dropStickerReact(ctx, chat, sid)
		}
		b.emitMessage(chat, m.ID)
		go takeBack()
		return
	}
	e, data, ok := b.stickerMessageOf(sticker)
	if !ok {
		return
	}
	msg := &waE2E.Message{StickerMessage: e, MessageContextInfo: &waE2E.MessageContextInfo{
		MessageSecret: messageSecret(),
		MessageAssociation: &waE2E.MessageAssociation{
			AssociationType:  waE2E.MessageAssociation_STICKER_ANNOTATION.Enum(),
			ParentMessageKey: cli.BuildMessageKey(jid, b.senderOf(m), m.ID),
		},
	}}
	r := stickerReact{sid: cli.GenerateMessageID(), who: meVoter, ts: b.sendTime().UnixMilli(), lottie: e.GetIsLottie(),
		payload: marshal(msg)}
	if err := b.store.putStickerReact(ctx, b.db, chat, m.ID, r); err != nil {
		b.log.Errorf("store sticker reaction: %v", err)
		return
	}
	for _, sid := range old {
		b.store.dropStickerReact(ctx, chat, sid)
	}
	// The picture is already on disk: the reaction shows it at once.
	if file, err := os.ReadFile(b.mediaPath(sticker.ChatID, sticker.ID)); err == nil {
		path := b.mediaPath(chat, r.sid)
		_ = os.MkdirAll(filepath.Dir(path), 0o700)
		_ = os.WriteFile(path, file, 0o600)
	}
	b.emitMessage(chat, m.ID)
	go func() {
		err := error(nil)
		if data != nil {
			err = b.uploadSticker(ctx, cli, e, data)
		}
		if err == nil {
			r.payload = marshal(msg)
			_ = b.store.putStickerReact(ctx, b.db, chat, m.ID, r)
			_, err = b.sendMessage(ctx, cli, jid, msg, whatsmeow.SendRequestExtra{ID: r.sid})
		}
		if err != nil {
			b.log.Warnf("react with a sticker in %s: %v", chat, err)
			b.store.dropStickerReact(ctx, chat, r.sid)
			b.emitMessage(chat, m.ID)
			b.emit(model.NoticeEvent{Text: "Couldn't send the reaction."})
			return
		}
		r.payload = marshal(msg) // sending may add to it
		_ = b.store.putStickerReact(ctx, b.db, chat, m.ID, r)
		b.recentSticker(marshal(e), time.Now(), chat, r.sid)
		takeBack()
	}()
}

// uploadSticker uploads a sticker's file and gives e its link and keys.
func (b *Backend) uploadSticker(ctx context.Context, cli *whatsmeow.Client, e *waE2E.StickerMessage, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	res, err := cli.Upload(ctx, data, whatsmeow.MediaImage)
	if err != nil {
		return err
	}
	e.URL, e.DirectPath, e.MediaKey = proto.String(res.URL), proto.String(res.DirectPath), res.MediaKey
	e.FileEncSHA256, e.FileSHA256, e.FileLength = res.FileEncSHA256, res.FileSHA256, proto.Uint64(res.FileLength)
	e.MediaKeyTimestamp = proto.Int64(time.Now().Unix())
	return nil
}

// stickerReactBlob is mediaBlob for a sticker reaction's sticker.
func (s *msgStore) stickerReactBlob(ctx context.Context, chat, sid string) ([]byte, error) {
	raw, err := s.stickerReactPayload(ctx, chat, sid)
	if err != nil {
		return nil, err
	}
	_, blob := rawMedia(raw)
	if blob == nil {
		return nil, errors.New("no sticker")
	}
	return blob, nil
}
