package wa

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/rand"
	"fmt"
	"strconv"
	"time"

	whatsmeow "github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waGroupHistory"
	"github.com/polymorfa/hypermeow/proto/waWeb"
	"github.com/polymorfa/hypermeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Sharing a group's recent messages with the people you add to it, as
// WhatsApp Web does (WAWebGenerateGroupHistoryBundleMsgData,
// WAWebSerializeGroupHistoryMessages): the messages, as history sync's
// WebMessageInfos written from your side, go in a zlib compressed
// waGroupHistory.GroupHistory, uploaded as group-history media under
// document keys. A messageHistoryBundle naming them as its receivers goes
// to the group, encrypted to their devices (and yours) alone, not with
// the group's sender key (SendRequestExtra.GroupRecipients, hypermeow's
// patch). See grouphistory.go for the other side.

const (
	// historyShareMax and historyShareWindow are how many of the newest
	// messages are shared, and from how far back: WhatsApp's phones share
	// up to 100 from the last 14 days.
	historyShareMax    = 100
	historyShareWindow = 14 * 24 * time.Hour
)

// historyShareRow is a stored message to share.
type historyShareRow struct {
	id, sender, push string
	fromMe           bool
	ts               int64
	raw, edit        []byte
}

// historyRows reads the messages of chat to share, oldest first.
func (s *msgStore) historyRows(ctx context.Context, chat string, since time.Time) ([]historyShareRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, sender_jid, sender_push, from_me, ts, raw_payload, edit_payload FROM (
		SELECT *, rowid AS rid FROM wz_messages
		WHERE chat = ? AND ts >= ? AND kind IN (?, ?, ?) AND whisper = '' AND quiet = 0 AND revoked = 0 AND raw_payload IS NOT NULL
		ORDER BY ts DESC, rid DESC LIMIT ?
	) ORDER BY ts, rid`, chat, since.Unix(), int(model.KindText), int(model.KindImage), int(model.KindSticker), historyShareMax)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []historyShareRow
	for rows.Next() {
		var r historyShareRow
		if err := rows.Scan(&r.id, &r.sender, &r.push, &r.fromMe, &r.ts, &r.raw, &r.edit); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// historyBundle makes the compressed GroupHistory of rows, sent to group
// by me, and the oldest message's time.
func historyBundle(group, me types.JID, rows []historyShareRow) ([]byte, int64, error) {
	var gh waGroupHistory.GroupHistory
	var oldest int64
	for _, r := range rows {
		payload := r.raw
		if len(r.edit) > 0 {
			payload = r.edit // as it reads now
		}
		var m waE2E.Message
		if proto.Unmarshal(payload, &m) != nil {
			continue
		}
		sender := r.sender
		if r.fromMe || sender == "" {
			sender = me.String()
		}
		w := &waWeb.WebMessageInfo{
			Key: &waCommon.MessageKey{RemoteJID: proto.String(group.String()), FromMe: proto.Bool(r.fromMe),
				ID: proto.String(r.id), Participant: proto.String(sender)},
			Message:          &m,
			MessageTimestamp: proto.Uint64(uint64(r.ts)),
			Participant:      proto.String(sender),
		}
		if r.push != "" && !r.fromMe {
			w.PushName = proto.String(r.push)
		}
		gh.Messages = append(gh.Messages, w)
		if oldest == 0 || r.ts < oldest {
			oldest = r.ts
		}
	}
	if len(gh.Messages) == 0 {
		return nil, 0, fmt.Errorf("no messages to share")
	}
	raw, err := proto.Marshal(&gh)
	if err != nil {
		return nil, 0, err
	}
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return nil, 0, err
	}
	if err := zw.Close(); err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), oldest, nil
}

// shareWithAdded shares group's recent messages with the people a GroupAdd
// added, on a goroutine of its own (it uploads).
func (b *Backend) shareWithAdded(cli *whatsmeow.Client, group types.JID, added []model.MemberResult) {
	var to []types.JID
	for _, r := range added {
		if j, err := types.ParseJID(r.ID); err == nil && r.Err == "" {
			to = append(to, j.ToNonAD())
		}
	}
	if len(to) == 0 {
		return
	}
	b.goWork(func() {
		if err := b.shareHistory(b.ctx, cli, group, to); err != nil {
			b.log.Warnf("share the history of %s: %v", group, err)
			b.emit(model.NoticeEvent{Text: "Couldn't share recent messages with the new members."})
		}
	})
}

// shareHistory shares group's recent messages with receivers, people just
// added to it, and notes it in the chat.
func (b *Backend) shareHistory(ctx context.Context, cli *whatsmeow.Client, group types.JID, receivers []types.JID) error {
	if len(receivers) == 0 {
		return nil
	}
	chat := group.String()
	rows, err := b.store.historyRows(ctx, chat, b.now().Add(-historyShareWindow))
	if err != nil {
		return err
	}
	me := b.ownJID(chat)
	data, oldest, err := historyBundle(group, me, rows)
	if err != nil {
		return err
	}
	up, err := cli.UploadGroupHistory(ctx, data)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	names := make([]string, len(receivers))
	for i, r := range receivers {
		names[i] = r.ToNonAD().String()
	}
	msg := &waE2E.Message{
		MessageHistoryBundle: &waE2E.MessageHistoryBundle{
			Mimetype:          proto.String("application/protobuf"),
			FileSHA256:        up.FileSHA256,
			FileEncSHA256:     up.FileEncSHA256,
			MediaKey:          up.MediaKey,
			DirectPath:        proto.String(up.DirectPath),
			MediaKeyTimestamp: proto.Int64(b.now().Unix()),
			MessageHistoryMetadata: &waE2E.MessageHistoryMetadata{
				HistoryReceivers:               names,
				MessageCount:                   proto.Int64(int64(len(rows))),
				OldestMessageTimestampInWindow: proto.Int64(oldest),
				OldestMessageTimestampInBundle: proto.Int64(oldest),
			},
		},
		MessageContextInfo: &waE2E.MessageContextInfo{MessageSecret: secret},
	}
	id := cli.GenerateMessageID()
	if _, err := b.sendMessage(ctx, cli, group, msg, whatsmeow.SendRequestExtra{ID: id, GroupRecipients: receivers}); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	params := []string{strconv.FormatInt(oldest, 10)}
	for _, r := range receivers {
		params = append(params, b.canonical(ctx, r).String())
	}
	b.putSystem(ctx, systemMsg(chat, id, b.now(), me, true, stubHistoryShared, params...))
	return nil
}
