package wa

import (
	"bytes"
	"cmp"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waGroupHistory"
	"github.com/polymorfa/hypermeow/proto/waWeb"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// A group member adding people can share the group's recent messages with
// them (WhatsApp's group history). Their phone uploads the messages as a
// file and sends the group a messageHistoryBundle that names who it's for
// (historyReceivers); the others get a messageHistoryNotice saying it was
// shared. The file, mms type "group-history" under "WhatsApp Document
// Keys", is a zlib (or gzip) compressed waGroupHistory.GroupHistory: the
// messages as history sync's WebMessageInfos, their keys written from the
// sharer's side (fromMe is the sharer). WhatsApp Web's modules
// (WAWebDownloadHistoryBundleAction, WAWebGroupHistoryBundleProcessor) do
// the same.

// stubHistoryShared is a group's recent messages shared with people added
// to it: a number of ours. Its parameters are the oldest shared message's
// time (unix seconds), then who got them.
const stubHistoryShared stubType = 100070

// maxGroupHistory caps a bundle's file once inflated.
const maxGroupHistory = 256 << 20

// historyConv is a chat's part of a history sync chunk, or of a group
// history bundle.
type historyConv struct {
	jid     types.JID
	isGroup bool
	name    string
	// meta is the chat's settings, nil to leave them as they are.
	meta  *chatMeta
	msgs  []storedMsg
	edits []parsed
	// receipts are who got your messages and when.
	receipts []personReceipt
	// votes are the votes in polls and answers to events.
	votes []historyVote
	// reactions are those history sync lists on the messages they react to.
	reactions []historyReaction
}

// historyMessages parses the history messages msgs of chat raw into cd,
// skipping those older than cutoff, and returns the group statuses among
// them. It may query the device store: never call it inside a msgStore
// transaction.
func (b *Backend) historyMessages(ctx context.Context, cli *whatsmeow.Client, raw types.JID, cd *historyConv,
	msgs []*waWeb.WebMessageInfo, cutoff time.Time) []storedStatus {
	var statuses []storedStatus
	jid := cd.jid
	for _, w := range msgs {
		if int64(w.GetMessageTimestamp()) < cutoff.Unix() {
			continue
		}
		if w.GetMessageStubType() != 0 {
			if m, ok := b.historyStub(ctx, jid, w); ok {
				cd.msgs = append(cd.msgs, m)
			}
			continue
		}
		evt, err := cli.ParseWebMessage(raw, w)
		if err != nil {
			continue
		}
		if isGroupStatus(evt) {
			if st, ok := b.parseStatus(ctx, evt); ok && st.revoke == "" {
				statuses = append(statuses, st)
			}
			continue
		}
		b.noteTheme(ctx, jid.String(), evt)
		p, ok := b.parse(ctx, evt)
		if !ok {
			continue
		}
		p.msg.ChatID = jid.String()
		if p.msg.FromMe {
			for _, ur := range w.GetUserReceipt() {
				who, err := types.ParseJID(ur.GetUserJID())
				if err != nil {
					continue
				}
				cd.receipts = append(cd.receipts, personReceipt{
					chat: p.msg.ChatID, id: p.msg.ID, who: b.canonical(ctx, who).String(),
					delivered: ur.GetReceiptTimestamp(), read: ur.GetReadTimestamp(), played: ur.GetPlayedTimestamp(),
				})
			}
		}
		if p.target != "" {
			cd.edits = append(cd.edits, p)
			continue
		}
		if pin, ok := historyPin(w, p.msg.ID); ok {
			cd.edits = append(cd.edits, pin)
		}
		// A message kept in a disappearing chat says so itself.
		p.msg.Kept = w.GetKeepInChat().GetKeepType() == waE2E.KeepType_KEEP_FOR_ALL
		if p.msg.Poll != nil || p.msg.Event != nil {
			for _, v := range b.historyVotes(ctx, raw, w) {
				cd.votes = append(cd.votes, historyVote{id: p.msg.ID, vote: v})
			}
		}
		for _, r := range w.GetReactions() {
			who, ok := b.voterOfKey(ctx, jid, r.GetKey())
			if ok && r.GetText() != "" {
				cd.reactions = append(cd.reactions, historyReaction{id: p.msg.ID,
					reaction: reaction{who: who, ts: r.GetSenderTimestampMS(), emoji: r.GetText()}})
			}
		}
		cd.msgs = append(cd.msgs, p.msg)
	}
	return statuses
}

// storeHistory stores what historyMessages parsed, then applies the edits,
// pins and the like among it. It reports false when nothing could be stored.
func (b *Backend) storeHistory(ctx context.Context, convs []historyConv, statuses []storedStatus) bool {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		b.log.Errorf("history sync: begin: %v", err)
		return false
	}
	for _, st := range statuses {
		if err := b.store.putStatus(ctx, tx, st); err != nil {
			b.log.Warnf("history sync: status %s: %v", st.id, err)
		}
	}
	for _, cd := range convs {
		jid := cd.jid.String()
		if err := b.store.ensureChat(ctx, tx, jid, cd.isGroup, cd.name); err != nil {
			b.log.Errorf("history sync: chat %s: %v", jid, err)
			continue
		}
		if cd.meta != nil {
			_ = b.store.setMeta(ctx, tx, jid, *cd.meta)
		}
		for _, m := range cd.msgs {
			if err := b.store.putMessage(ctx, tx, m); err != nil {
				b.log.Warnf("history sync: message %s: %v", m.ID, err)
			}
		}
		for _, r := range cd.receipts {
			if err := b.store.putReceipt(ctx, tx, r); err != nil {
				b.log.Warnf("history sync: receipt of %s: %v", r.id, err)
			}
		}
		for _, v := range cd.votes {
			if err := b.store.putVote(ctx, tx, jid, v.id, v.vote); err != nil {
				b.log.Warnf("history sync: vote on %s: %v", v.id, err)
			}
		}
		for _, r := range cd.reactions {
			if err := b.store.putReaction(ctx, tx, jid, r.id, r.reaction); err != nil {
				b.log.Warnf("history sync: reaction to %s: %v", r.id, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		b.log.Errorf("history sync: commit: %v", err)
		return false
	}
	for _, cd := range convs {
		chat := cd.jid.String()
		var pins []parsed
		for _, p := range cd.edits {
			switch {
			case p.revoke:
				b.revoke(ctx, chat, p)
			case p.edited:
				_ = b.store.editText(ctx, chat, p.target, p.edit, p.editTime, p.msg.rawPayload)
			case p.pin != 0:
				pins = append(pins, p)
			case p.keep != 0:
				_ = b.store.setMessageFlag(ctx, chat, p.target, "kept", p.keep > 0)
			case p.vote != nil:
				_ = b.store.putVote(ctx, b.db, chat, p.target, *p.vote)
			case p.event != nil:
				_ = b.store.editEvent(ctx, chat, p.target, p.event, p.msg.rawPayload)
			case p.reaction != nil:
				_ = b.store.putReaction(ctx, b.db, chat, p.target, *p.reaction)
			case p.sticker != nil:
				_ = b.store.putStickerReact(ctx, b.db, chat, p.target, *p.sticker)
			case p.comment != nil:
				_ = b.store.putComment(ctx, b.db, chat, p.target, *p.comment)
			}
		}
		// In the order they were made: history lists the newest first.
		slices.SortStableFunc(pins, func(a, b parsed) int { return cmp.Compare(a.pinAt, b.pinAt) })
		for _, p := range pins {
			_ = b.store.setPin(ctx, b.db, chat, b.albumTarget(ctx, chat, p.target), p.pin > 0, p.pinAt, p.pinUntil)
		}
	}
	return true
}

// historyShared turns a messageHistoryBundle (bundle set) or
// messageHistoryNotice of a group into its system message. A bundle shows
// to those it's for and a notice to everyone else, so each member sees it
// once.
func (b *Backend) historyShared(ctx context.Context, evt *events.Message, chat types.JID,
	md *waE2E.MessageHistoryMetadata, bundle bool) (storedMsg, bool) {
	if chat.Server != types.GroupServer || evt.Info.ID == "" || md == nil {
		return storedMsg{}, false
	}
	params := []string{strconv.FormatInt(md.GetOldestMessageTimestampInWindow(), 10)}
	forMe := false
	for _, r := range md.GetHistoryReceivers() {
		j, err := types.ParseJID(r)
		if err != nil {
			continue
		}
		if b.isMe(j) {
			forMe = true
			params = append(params, b.ownJID(chat.String()).String())
		} else {
			params = append(params, b.canonical(ctx, j).String())
		}
	}
	if forMe != bundle {
		return storedMsg{}, false
	}
	actor := b.canonical(ctx, evt.Info.Sender)
	return systemMsg(chat.String(), evt.Info.ID, evt.Info.Timestamp, actor, evt.Info.IsFromMe,
		stubHistoryShared, params...), true
}

// historySharedText words a stubHistoryShared system message: actor
// (empty when unknown) shared the messages since params[0] with the rest.
func historySharedText(actor string, params []string, object func(string) string) string {
	since := ""
	if len(params) > 0 {
		if ts, err := strconv.ParseInt(params[0], 10, 64); err == nil && ts > 0 {
			since = " since " + time.Unix(ts, 0).Local().Format("Jan 2, 2006, 3:04 PM")
		}
	}
	var names []string
	if len(params) > 1 {
		for _, p := range params[1:] {
			if n := object(p); n != "" {
				names = append(names, n)
			}
		}
	}
	who := listNames(names)
	if who == "" {
		who = "new members"
	}
	if actor == "" {
		return "Recent messages" + since + " were shared with " + who
	}
	return actor + " shared recent messages" + since + " with " + who
}

// onHistoryBundle fetches the messages a group history bundle shares with
// you and adds them to the group, once.
func (b *Backend) onHistoryBundle(e *events.Message, hb *waE2E.MessageHistoryBundle) {
	if !e.Info.IsGroup || e.Info.IsFromMe || e.Info.ID == "" || hb.GetDirectPath() == "" {
		return
	}
	forMe := false
	for _, r := range hb.GetMessageHistoryMetadata().GetHistoryReceivers() {
		if j, err := types.ParseJID(r); err == nil && b.isMe(j) {
			forMe = true
		}
	}
	if !forMe {
		return
	}
	ctx := b.ctx
	chat := b.canonical(ctx, e.Info.Chat)
	done := "group_history:" + chat.String() + ":" + e.Info.ID
	if b.store.meta(ctx, done) != "" {
		return
	}
	raw, sender := e.Info.Chat, e.Info.Sender.ToNonAD()
	b.goWork(func() {
		cli := b.client()
		if cli == nil {
			return
		}
		data, err := cli.DownloadMediaWithPath(ctx, hb.GetDirectPath(), hb.GetFileEncSHA256(), hb.GetFileSHA256(),
			hb.GetMediaKey(), whatsmeow.MediaDocument, "group-history", false)
		if err != nil {
			b.log.Warnf("group history %s in %s: download: %v", e.Info.ID, chat, err)
			return
		}
		gh, err := decodeGroupHistory(data)
		if err != nil {
			b.log.Warnf("group history %s in %s: %v", e.Info.ID, chat, err)
			_ = b.store.setMetaValue(ctx, done, "failed")
			return
		}
		n := b.injectGroupHistory(ctx, cli, raw, chat, sender, gh)
		_ = b.store.setMetaValue(ctx, done, strconv.Itoa(n))
		b.log.Infof("group history %s in %s: %d messages from %s", e.Info.ID, chat, n, sender)
	})
}

// decodeGroupHistory reads a group history bundle's file.
func decodeGroupHistory(data []byte) (*waGroupHistory.GroupHistory, error) {
	var r io.Reader
	var err error
	switch {
	case len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b:
		r, err = gzip.NewReader(bytes.NewReader(data))
	case len(data) >= 2 && data[0]&0x0f == 8 && (uint16(data[0])<<8|uint16(data[1]))%31 == 0:
		r, err = zlib.NewReader(bytes.NewReader(data))
	default:
		r = flate.NewReader(bytes.NewReader(data))
	}
	if err != nil {
		return nil, fmt.Errorf("inflate: %w", err)
	}
	inflated, err := io.ReadAll(io.LimitReader(r, maxGroupHistory+1))
	if err != nil {
		return nil, fmt.Errorf("inflate: %w", err)
	}
	if len(inflated) > maxGroupHistory {
		return nil, errors.New("too big")
	}
	var gh waGroupHistory.GroupHistory
	if err := proto.Unmarshal(inflated, &gh); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &gh, nil
}

// injectGroupHistory adds the messages of bundle gh, which sender shared,
// to group chat (raw as the stanza named it) and returns how many it
// stored. They count as history: never unread, never notified.
func (b *Backend) injectGroupHistory(ctx context.Context, cli *whatsmeow.Client, raw, chat, sender types.JID,
	gh *waGroupHistory.GroupHistory) int {
	var msgs []*waWeb.WebMessageInfo
	msgs = append(msgs, gh.GetMessages()...)
	msgs = append(msgs, gh.GetOutOfWindowPinnedMessages()...)
	for _, l := range gh.GetUncountedAssociatedMessageLists() {
		msgs = append(msgs, l.GetMessages()...)
	}
	msgs = append(msgs, gh.GetCommentMessages()...)
	if t := gh.GetThemeMessage(); t != nil {
		msgs = append(msgs, t)
	}
	kept := msgs[:0]
	for _, w := range msgs {
		if w.GetKey().GetID() == "" {
			continue
		}
		if rj := w.GetKey().GetRemoteJID(); rj != "" {
			if j, err := types.ParseJID(rj); err != nil || j.User != raw.User || j.Server != types.GroupServer {
				continue // not this group's
			}
		}
		b.localizeKeys(w, sender)
		kept = append(kept, w)
	}
	cd := historyConv{jid: chat, isGroup: true}
	statuses := b.historyMessages(ctx, cli, raw, &cd, kept, time.Time{})
	if !b.storeHistory(ctx, []historyConv{cd}, statuses) {
		return 0
	}
	b.emitAllMessages(chat.String())
	b.emitChat(chat.String())
	return len(cd.msgs)
}

// localizeKeys turns the keys in w, a message of a group history bundle
// written from its sender's side, into this device's: a key that was the
// sender's own is sender's, and one of yours is yours.
func (b *Backend) localizeKeys(w *waWeb.WebMessageInfo, sender types.JID) {
	wasSenders := w.GetKey().GetFromMe()
	walkKeys(w.ProtoReflect(), func(k *waCommon.MessageKey) {
		if k.GetFromMe() {
			k.FromMe = proto.Bool(false)
			k.Participant = proto.String(sender.String())
		}
		if j, err := types.ParseJID(k.GetParticipant()); err == nil && k.GetParticipant() != "" && b.isMe(j) {
			k.FromMe = proto.Bool(true)
		}
	})
	if wasSenders {
		w.Participant = proto.String(sender.String())
	}
	// ParseWebMessage would take a message of yours as written by this.
	w.OriginalSelfAuthorUserJIDString = nil
	if k := w.GetKey(); k != nil && !k.GetFromMe() {
		if j, err := types.ParseJID(w.GetParticipant()); err == nil && w.GetParticipant() != "" && b.isMe(j) {
			k.FromMe = proto.Bool(true)
		}
	}
}

// walkKeys calls f with every message key in m.
func walkKeys(m protoreflect.Message, f func(*waCommon.MessageKey)) {
	if k, ok := m.Interface().(*waCommon.MessageKey); ok {
		f(k)
		return
	}
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case fd.IsMap() || fd.Message() == nil:
		case fd.IsList():
			l := v.List()
			for i := range l.Len() {
				walkKeys(l.Get(i).Message(), f)
			}
		default:
			walkKeys(v.Message(), f)
		}
		return true
	})
}
