package wa

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/appstate"
	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waSyncAction"
	"github.com/polymorfa/hypermeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// ownJID is how you appear in a chat: your phone number in chats addressed
// by phone number, your LID everywhere else.
func (b *Backend) ownJID(chatID string) types.JID {
	cli := b.client()
	if cli == nil || cli.Store.ID == nil {
		return types.EmptyJID
	}
	pn := cli.Store.ID.ToNonAD()
	lid := cli.Store.GetLID().ToNonAD()
	if j, err := types.ParseJID(chatID); err == nil && j.Server == types.DefaultUserServer || lid.IsEmpty() {
		return pn
	}
	return lid
}

// senderOf returns the author of a message.
func (b *Backend) senderOf(m *model.Message) types.JID {
	if m.FromMe {
		return b.ownJID(m.ChatID)
	}
	if j, err := types.ParseJID(m.SenderID); err == nil && !j.IsEmpty() {
		return j.ToNonAD()
	}
	j, _ := types.ParseJID(m.ChatID)
	return j
}

// connected returns the client if it is online, reporting a toast if not.
func (b *Backend) connected() *whatsmeow.Client {
	cli := b.client()
	if cli == nil || !cli.IsConnected() {
		b.emit(model.NoticeEvent{Text: "You're offline. Try again once connected."})
		return nil
	}
	return cli
}

// quotedMessage rebuilds the content of a stored message for a reply's
// context. Media keep their original (downloadable) message; everything
// else is quoted as text.
func (b *Backend) quotedMessage(ctx context.Context, chatID, id string) *waE2E.Message {
	if chatID == statusChat {
		return b.store.statusMessage(ctx, id)
	}
	r, ok := b.store.message(ctx, chatID, id)
	if !ok {
		return &waE2E.Message{Conversation: proto.String("")}
	}
	if media, blob, err := b.store.mediaBlob(ctx, chatID, id); err == nil && len(blob) > 0 {
		if m := mediaMessage(media, blob); m != nil {
			return m
		}
	}
	switch r.Media {
	case model.MediaLocation, model.MediaContact, model.MediaPoll, model.MediaEventInvite:
		// Quoted as what it is, so the quote shows a poll as a poll.
		if pm, err := b.rawMessage(chatID, id); err == nil {
			if q := cardOf(pm); q != nil {
				return q
			}
		}
	}
	return &waE2E.Message{Conversation: proto.String(r.Text)}
}

// cardOf returns the place, contact cards, poll or event in m, without
// what they reply to or mention, as a message of its own; or nil.
func cardOf(m *waE2E.Message) *waE2E.Message {
	m = unwrap(m)
	c := &waE2E.Message{
		LocationMessage: m.GetLocationMessage(), LiveLocationMessage: m.GetLiveLocationMessage(),
		ContactMessage: m.GetContactMessage(), ContactsArrayMessage: m.GetContactsArrayMessage(),
		PollCreationMessage: m.GetPollCreationMessage(), PollCreationMessageV2: m.GetPollCreationMessageV2(),
		PollCreationMessageV3: m.GetPollCreationMessageV3(), PollCreationMessageV5: m.GetPollCreationMessageV5(),
		PollCreationMessageV6: m.GetPollCreationMessageV6(), EventMessage: m.GetEventMessage(),
	}
	if proto.Size(c) == 0 {
		return nil
	}
	c = proto.Clone(c).(*waE2E.Message)
	for _, p := range []*waE2E.PollCreationMessage{c.PollCreationMessage, c.PollCreationMessageV2,
		c.PollCreationMessageV3, c.PollCreationMessageV5, c.PollCreationMessageV6} {
		if p != nil {
			p.ContextInfo = nil
		}
	}
	if c.LocationMessage != nil {
		c.LocationMessage.ContextInfo = nil
	}
	if c.LiveLocationMessage != nil {
		c.LiveLocationMessage.ContextInfo = nil
	}
	if c.ContactMessage != nil {
		c.ContactMessage.ContextInfo = nil
	}
	if c.ContactsArrayMessage != nil {
		c.ContactsArrayMessage.ContextInfo = nil
	}
	if c.EventMessage != nil {
		c.EventMessage.ContextInfo = nil
	}
	return c
}

// mediaMessage turns a stored media blob back into a sendable message.
func mediaMessage(media model.Media, blob []byte) *waE2E.Message {
	var (
		m   waE2E.Message
		err error
	)
	switch media {
	case model.MediaImage:
		m.ImageMessage = &waE2E.ImageMessage{}
		err = proto.Unmarshal(blob, m.ImageMessage)
	case model.MediaSticker:
		m.StickerMessage = &waE2E.StickerMessage{}
		err = proto.Unmarshal(blob, m.StickerMessage)
	case model.MediaVideo, model.MediaGIF:
		m.VideoMessage = &waE2E.VideoMessage{}
		err = proto.Unmarshal(blob, m.VideoMessage)
	case model.MediaVoice, model.MediaAudio:
		m.AudioMessage = &waE2E.AudioMessage{}
		err = proto.Unmarshal(blob, m.AudioMessage)
	case model.MediaDocument:
		m.DocumentMessage = &waE2E.DocumentMessage{}
		err = proto.Unmarshal(blob, m.DocumentMessage)
	default:
		return nil
	}
	if err != nil {
		return nil
	}
	return &m
}

// setContext attaches ci to whichever content m carries.
func setContext(m *waE2E.Message, ci *waE2E.ContextInfo) {
	switch {
	case m.ImageMessage != nil:
		m.ImageMessage.ContextInfo = ci
	case m.StickerMessage != nil:
		m.StickerMessage.ContextInfo = ci
	case m.VideoMessage != nil:
		m.VideoMessage.ContextInfo = ci
	case m.AudioMessage != nil:
		m.AudioMessage.ContextInfo = ci
	case m.DocumentMessage != nil:
		m.DocumentMessage.ContextInfo = ci
	case m.ExtendedTextMessage != nil:
		m.ExtendedTextMessage.ContextInfo = ci
	}
}

// Send stores the message as pending, returns it, and sends it in the background.
func (b *Backend) Send(chatID string, d model.Draft) *model.Message {
	jid, err := types.ParseJID(chatID)
	cli := b.client()
	if err != nil || cli == nil {
		return nil
	}
	m := &model.Message{
		ID:      cli.GenerateMessageID(),
		ChatID:  chatID,
		FromMe:  true,
		Text:    d.Text,
		Time:    b.sendTime(),
		Receipt: model.Pending,
	}
	sm := storedMsg{Message: m}
	msg := &waE2E.Message{Conversation: proto.String(d.Text)}
	link := d.Link.Shown(d.Text)
	if link {
		m.Link, m.Thumb = d.Link, d.LinkThumb
		if img := d.LinkImage; len(img.Data) > 0 {
			l := *d.Link
			l.W, l.H = img.W, img.H
			m.Link = &l
			// Ours to show at once; the upload makes it the others'.
			path := b.mediaPath(chatID, m.ID)
			_ = os.MkdirAll(filepath.Dir(path), 0o700)
			if err := os.WriteFile(path, img.Data, 0o600); err != nil {
				b.log.Warnf("save link preview picture: %v", err)
			}
		}
	}
	if ci := b.draftContext(chatID, d, m); ci != nil || link {
		e := &waE2E.ExtendedTextMessage{
			Text:                  proto.String(d.Text),
			ContextInfo:           ci,
			InviteLinkGroupTypeV2: waE2E.ExtendedTextMessage_DEFAULT.Enum(),
		}
		var prep func(context.Context)
		if link {
			addLink(e, d.Link, d.LinkThumb)
			if img := d.LinkImage; len(img.Data) > 0 {
				// Its size makes the card wide at once (see describe).
				e.ThumbnailWidth, e.ThumbnailHeight = proto.Uint32(uint32(img.W)), proto.Uint32(uint32(img.H))
				prep = func(ctx context.Context) { b.uploadLinkImage(ctx, cli, e, img) }
			}
		}
		msg = &waE2E.Message{ExtendedTextMessage: e}
		if d.MentionAdmins || d.MentionChat != "" {
			msg = &waE2E.Message{GroupMentionedMessage: &waE2E.FutureProofMessage{Message: msg}}
		}
		return b.storeAndSendAfter(jid, sm, msg, nil, prep)
	}
	return b.storeAndSend(jid, sm, msg, nil)
}

// quote makes a message sent to chatID a reply to r: it fills in ci and
// returns the quote the message shows.
func (b *Backend) quote(chatID string, r *model.Message, ci *waE2E.ContextInfo) *model.Quote {
	sender := b.senderOf(r)
	ci.StanzaID = proto.String(r.ID)
	ci.Participant = proto.String(sender.String())
	ci.QuotedMessage = b.quotedMessage(b.ctx, r.ChatID, r.ID)
	if r.ChatID != chatID {
		// "Reply privately" quotes a group message in a one-to-one chat.
		ci.RemoteJID = proto.String(r.ChatID)
	}
	q := &model.Quote{ID: r.ID, SenderID: sender.String(), Text: r.Text, Media: r.Media}
	// Store the raw quoted text, not the display text with resolved names.
	if raw, ok := b.store.message(b.ctx, r.ChatID, r.ID); ok {
		q.Text = raw.Text
	}
	return q
}

// storeAndSend stores an outgoing message as pending, sends it in the
// background and returns it.
//
// If the server rejects msg and fallback is given, fallback is sent instead.
func (b *Backend) storeAndSend(jid types.JID, sm storedMsg, msg, fallback *waE2E.Message) *model.Message {
	return b.storeAndSendAfter(jid, sm, msg, fallback, nil)
}

// storeAndSendAfter is storeAndSend that first runs prep, if not nil, on
// the sending goroutine: an upload that fills in msg.
func (b *Backend) storeAndSendAfter(jid types.JID, sm storedMsg, msg, fallback *waE2E.Message, prep func(context.Context)) *model.Message {
	sm.rawPayload = marshal(msg)
	ctx, chatID, m := b.ctx, sm.ChatID, sm.Message
	if err := b.store.ensureChat(ctx, b.db, chatID, jid.Server == types.GroupServer, ""); err != nil {
		b.log.Errorf("store chat %s: %v", chatID, err)
	}
	if err := b.store.putMessage(ctx, b.db, sm); err != nil {
		b.log.Errorf("store outgoing message: %v", err)
	}
	b.emitChat(chatID)
	b.sendAsyncPrep(chatID, jid, m.ID, msg, fallback, prep)
	if r, ok := b.store.message(ctx, chatID, m.ID); ok {
		return b.resolve(ctx, r, jid.Server == types.GroupServer)
	}
	cp := *m
	return &cp
}

// sendAsync sends msg in the background and marks it sent when done.
func (b *Backend) sendAsync(chatID string, jid types.JID, id string, msg *waE2E.Message) {
	b.sendAsyncOr(chatID, jid, id, msg, nil)
}

// sendAsyncOr is sendAsync that sends fallback instead when the server
// rejects msg.
func (b *Backend) sendAsyncOr(chatID string, jid types.JID, id string, msg, fallback *waE2E.Message) {
	b.sendAsyncPrep(chatID, jid, id, msg, fallback, nil)
}

// sendAsyncPrep is sendAsyncOr that runs prep, if not nil, before sending.
func (b *Backend) sendAsyncPrep(chatID string, jid types.JID, id string, msg, fallback *waE2E.Message, prep func(context.Context)) {
	cli := b.client()
	go func() {
		if prep != nil {
			prep(b.ctx)
		}
		if err := b.store.setRawPayload(b.ctx, chatID, id, msg); err != nil {
			b.sendFailed(chatID, id, "Couldn't store the message payload.")
			return
		}
		resp, err := b.sendMessage(b.ctx, cli, jid, msg, whatsmeow.SendRequestExtra{ID: id})
		// SendMessage can add a message secret and other protocol metadata.
		_ = b.store.setRawPayload(b.ctx, chatID, id, msg)
		if err != nil && fallback != nil {
			b.log.Warnf("send to %s: %v; sending the fallback", chatID, err)
			resp, err = b.sendMessage(b.ctx, cli, jid, fallback, whatsmeow.SendRequestExtra{ID: id})
			if err == nil {
				_ = b.store.setRawPayload(b.ctx, chatID, id, fallback)
			}
		}
		if err != nil {
			b.log.Errorf("send to %s: %v", chatID, err)
			b.sendFailed(chatID, id, "Couldn't send the message.")
			return
		}
		_ = b.store.setReceipt(b.ctx, chatID, []string{id}, model.Sent)
		b.emit(model.ReceiptEvent{ChatID: chatID, IDs: []string{id}, Receipt: model.Sent})
		b.serverTime(chatID, id, resp.Timestamp)
	}()
}

// serverTime gives a sent message the time the server took it at, which
// is what the others see and what their messages carry, so it sorts among
// theirs. Our own clock can be seconds off, and within a second ours had
// milliseconds theirs lack. The skew is kept for the next sends.
func (b *Backend) serverTime(chatID, id string, at time.Time) {
	if at.IsZero() {
		return
	}
	b.serverSkew.Store(at.Unix() - b.now().Unix())
	changed, err := b.store.setTime(b.ctx, chatID, id, at)
	if err != nil {
		b.log.Warnf("set the server time of %s: %v", id, err)
	}
	if changed {
		b.emitMessage(chatID, id)
	}
}

// sendFailed marks a pending message failed, so it stops showing a clock,
// and says so.
func (b *Backend) sendFailed(chatID, id, notice string) {
	if err := b.store.setFailed(b.ctx, chatID, id); err != nil {
		b.log.Errorf("mark %s failed: %v", id, err)
	}
	b.emit(model.ReceiptEvent{ChatID: chatID, IDs: []string{id}, Receipt: model.Failed})
	b.emit(model.NoticeEvent{Text: notice})
}

// sendCopy sends an existing message's content to another chat, as a
// sticker/forward, and stores the copy. A sticker can reply to a message.
func (b *Backend) sendCopy(src *model.Message, chatID string, forwarded bool, reply *model.Message) bool {
	ctx := b.ctx
	cli := b.client()
	jid, err := types.ParseJID(chatID)
	if err != nil || cli == nil {
		return false
	}
	var msg *waE2E.Message
	media, blob, _ := b.store.mediaBlob(ctx, src.ChatID, src.ID)
	raw, ok := b.store.message(ctx, src.ChatID, src.ID)
	if src.ChatID == stickerChat {
		raw, ok = rawMsg{Message: &model.Message{Kind: model.KindSticker, Media: model.MediaSticker}}, true
	}
	if !ok {
		return false
	}
	if len(blob) > 0 {
		msg = mediaMessage(media, blob)
	}
	var ci *waE2E.ContextInfo
	if forwarded {
		ci = &waE2E.ContextInfo{IsForwarded: proto.Bool(true), ForwardingScore: proto.Uint32(1)}
	}
	if reply != nil {
		if ci == nil {
			ci = &waE2E.ContextInfo{}
		}
		b.quote(chatID, reply, ci)
	}
	if msg == nil && raw.Kind != model.KindDeleted {
		msg = cardMessage(raw.Message)
	}
	switch {
	case msg != nil:
		setContext(msg, ci)
	case raw.Media != model.MediaNone || raw.Kind == model.KindDeleted || raw.Text == "":
		return false // media we can't resend
	default:
		e := &waE2E.ExtendedTextMessage{Text: proto.String(raw.Text), ContextInfo: ci}
		if raw.Link.Shown(raw.Text) {
			addLink(e, raw.Link, raw.Thumb)
			if media == model.MediaNone {
				copyLinkImage(e, blob)
			}
		}
		msg = &waE2E.Message{ExtendedTextMessage: e}
	}
	m := &model.Message{
		ID: cli.GenerateMessageID(), ChatID: chatID, FromMe: true, Time: b.sendTime(), Receipt: model.Pending,
		Kind: raw.Kind, Media: raw.Media, Text: raw.Text,
	}
	sm := storedMsg{Message: m, rawPayload: marshal(msg)}
	if err := b.store.putMessage(ctx, b.db, sm); err != nil {
		b.log.Errorf("store forwarded message: %v", err)
	}
	// The picture is already on disk; share it with the copy.
	if data, err := os.ReadFile(b.mediaPath(src.ChatID, src.ID)); err == nil {
		_ = os.WriteFile(b.mediaPath(chatID, m.ID), data, 0o600)
	}
	b.sendAsync(chatID, jid, m.ID, msg)
	if r, ok := b.store.message(ctx, chatID, m.ID); ok {
		b.emit(model.MessageEvent{Msg: b.resolve(ctx, r, jid.Server == types.GroupServer)})
	}
	b.emitChat(chatID)
	return true
}

// Forward implements model.Backend.
func (b *Backend) Forward(msgs []*model.Message, chatIDs []string) {
	if b.connected() == nil {
		return
	}
	skipped := 0
	for _, c := range chatIDs {
		for _, m := range msgs {
			if !b.sendCopy(m, c, true, nil) {
				skipped++
			}
		}
	}
	if skipped > 0 {
		b.emit(model.NoticeEvent{Text: "Some messages couldn't be forwarded."})
	}
}

// SendSticker implements model.Backend. The sticker becomes a recent one.
func (b *Backend) SendSticker(chatID string, sticker, reply *model.Message) {
	if sticker.ChatID == stickerChat && strings.HasPrefix(sticker.ID, encStickerPrefix) {
		// Its plaintext hash, which a sent sticker carries, is known only
		// once it has downloaded and been re-keyed (rehashSticker).
		b.emit(model.NoticeEvent{Text: "This sticker is still loading."})
		return
	}
	if b.connected() == nil || !b.sendCopy(sticker, chatID, false, reply) {
		return
	}
	if _, blob, err := b.store.mediaBlob(b.ctx, sticker.ChatID, sticker.ID); err == nil {
		b.recentSticker(blob, time.Now(), sticker.ChatID, sticker.ID)
	}
}

// React implements model.Backend.
func (b *Backend) React(m *model.Message, emoji string) {
	cli := b.connected()
	jid, err := types.ParseJID(m.ChatID)
	if cli == nil || err != nil {
		return
	}
	_ = b.store.putReaction(b.ctx, b.db, m.ChatID, m.ID, reaction{who: meVoter, ts: b.sendTime().UnixMilli(), emoji: emoji})
	b.emitMessage(m.ChatID, m.ID)
	msg := cli.BuildReaction(jid, b.senderOf(m), m.ID, emoji)
	go func() {
		if _, err := b.sendMessage(b.ctx, cli, jid, msg); err != nil {
			b.log.Warnf("react in %s: %v", m.ChatID, err)
			b.emit(model.NoticeEvent{Text: "Couldn't send the reaction."})
		}
	}()
}

// emitMessage re-reads a message and sends it to the UI.
func (b *Backend) emitMessage(chatID, id string) {
	if r, ok := b.store.message(b.ctx, chatID, id); ok {
		j, _ := types.ParseJID(chatID)
		b.emit(model.MessageEvent{Msg: b.resolve(b.ctx, r, j.Server == types.GroupServer)})
	}
}

// messageKey returns the key of a stored message and its chat JID.
func (b *Backend) messageKey(m *model.Message) (types.JID, *waCommon.MessageKey) {
	jid, _ := types.ParseJID(m.ChatID)
	return jid, b.client().BuildMessageKey(jid, b.senderOf(m), m.ID)
}

// Delete implements model.Backend.
func (b *Backend) Delete(m *model.Message, forEveryone bool) {
	cli := b.connected()
	if cli == nil {
		return
	}
	ctx := b.ctx
	jid, _ := types.ParseJID(m.ChatID)
	if forEveryone {
		sender, by := types.EmptyJID, ""
		if !m.FromMe {
			sender = b.senderOf(m) // as a group admin
			by = b.ownJID(m.ChatID).String()
		}
		_ = b.store.markDeleted(ctx, m.ChatID, m.ID, by)
		b.emitMessage(m.ChatID, m.ID)
		b.emitChat(m.ChatID)
		go func() {
			if _, err := b.sendMessage(ctx, cli, jid, cli.BuildRevoke(jid, sender, m.ID)); err != nil {
				b.log.Warnf("revoke in %s: %v", m.ChatID, err)
				b.emit(model.NoticeEvent{Text: "Couldn't delete the message for everyone."})
			}
		}()
		return
	}
	_ = b.store.deleteMessage(ctx, m.ChatID, m.ID)
	b.emit(model.DeletedEvent{ChatID: m.ChatID, IDs: []string{m.ID}})
	b.emitChat(m.ChatID)
	participant := "0"
	if !m.FromMe && jid.Server == types.GroupServer {
		participant = b.senderOf(m).String()
	}
	patch := appstate.PatchInfo{
		Type: appstate.WAPatchRegularHigh,
		Mutations: []appstate.MutationInfo{{
			Index:   []string{appstate.IndexDeleteMessageForMe, jid.String(), m.ID, fromMeFlag(m.FromMe), participant},
			Version: 3,
			Value: &waSyncAction.SyncActionValue{DeleteMessageForMeAction: &waSyncAction.DeleteMessageForMeAction{
				DeleteMedia:      proto.Bool(true),
				MessageTimestamp: proto.Int64(m.Time.Unix()),
			}},
		}},
	}
	b.sendAppState(patch)
}

func fromMeFlag(fromMe bool) string {
	if fromMe {
		return "1"
	}
	return "0"
}

// sendAppState sends a patch in the background.
func (b *Backend) sendAppState(patch appstate.PatchInfo) {
	cli := b.client()
	go b.sendAppStateNow(cli, patch)
}

// sendAppStateNow sends a patch and waits for it, so several patches go
// one after the other.
func (b *Backend) sendAppStateNow(cli *whatsmeow.Client, patch appstate.PatchInfo) {
	if err := cli.SendAppState(b.ctx, patch); err != nil {
		b.log.Warnf("send app state %s: %v", patch.Type, err)
		if errors.Is(err, appstate.ErrMismatchingLTHash) {
			// requestAppStateRecovery is on it (AppStateSyncError).
			b.emit(model.NoticeEvent{Text: "Your phone is repairing sync. Try again in a minute."})
			return
		}
		b.emit(model.NoticeEvent{Text: "Couldn't sync the change to your phone."})
	}
}

// Star implements model.Backend.
func (b *Backend) Star(m *model.Message, starred bool) {
	cli := b.connected()
	if cli == nil {
		return
	}
	_ = b.store.setMessageFlag(b.ctx, m.ChatID, m.ID, "starred", starred)
	b.emitMessage(m.ChatID, m.ID)
	jid, _ := types.ParseJID(m.ChatID)
	sender := jid // "0" in the index: one-to-one chats and your own messages
	if jid.Server == types.GroupServer && !m.FromMe {
		sender = b.senderOf(m)
	}
	b.sendAppState(appstate.BuildStar(jid, sender, m.ID, m.FromMe, starred))
}

// OpenedViewOnce implements model.Backend. Only this computer hears of
// it: the phone keeps its own view once messages.
func (b *Backend) OpenedViewOnce(m *model.Message) {
	_ = b.store.setMessageFlag(b.ctx, m.ChatID, m.ID, "opened", true)
	b.emitMessage(m.ChatID, m.ID)
}

// pinDuration is how long a pinned message stays pinned (WhatsApp offers
// 24 hours, 7 days or 30 days; the desktop app defaults to 7 days).
const pinDuration = 7 * 24 * time.Hour

// PinMessage implements model.Backend.
func (b *Backend) PinMessage(m *model.Message, pinned bool) {
	cli := b.connected()
	if cli == nil {
		return
	}
	ctx := b.ctx
	jid, key := b.messageKey(m)
	typ := waE2E.PinInChatMessage_PIN_FOR_ALL
	if !pinned {
		typ = waE2E.PinInChatMessage_UNPIN_FOR_ALL
	}
	msg := &waE2E.Message{
		PinInChatMessage: &waE2E.PinInChatMessage{
			Key: key, Type: typ.Enum(), SenderTimestampMS: proto.Int64(time.Now().UnixMilli()),
		},
		MessageContextInfo: &waE2E.MessageContextInfo{
			MessageAddOnDurationInSecs: proto.Uint32(uint32(pinDuration / time.Second)),
		},
	}
	if pinned {
		_, _ = b.db.ExecContext(ctx, `UPDATE wz_messages SET pinned = 0 WHERE chat = ?`, m.ChatID)
	}
	_ = b.store.setMessageFlag(ctx, m.ChatID, m.ID, "pinned", pinned)
	b.emitAllMessages(m.ChatID)
	go func() {
		if _, err := b.sendMessage(ctx, cli, jid, msg); err != nil {
			b.log.Warnf("pin in %s: %v", m.ChatID, err)
			b.emit(model.NoticeEvent{Text: "Couldn't pin the message."})
		}
	}()
}

// emitAllMessages asks the UI to reload an open chat.
func (b *Backend) emitAllMessages(chatID string) {
	b.emit(model.DeletedEvent{ChatID: chatID})
}

// SaveMedia implements model.Backend.
func (b *Backend) SaveMedia(m *model.Message) {
	go func() {
		ctx, cancel := context.WithTimeout(b.ctx, 5*time.Minute)
		defer cancel()
		data, err := os.ReadFile(b.mediaPath(m.ChatID, m.ID))
		if err != nil {
			data, err = os.ReadFile(b.mediaFilePath(m))
		}
		media, blob, _ := b.store.mediaBlob(ctx, m.ChatID, m.ID)
		if err != nil {
			dl := mediaMessage(media, blob)
			cli := b.client()
			if dl == nil || cli == nil || !cli.IsConnected() {
				b.emit(model.NoticeEvent{Text: "This media isn't available."})
				return
			}
			if data, err = cli.DownloadAny(ctx, dl); err != nil {
				b.log.Infof("download for saving %s: %v", m.ID, err)
				b.emit(model.NoticeEvent{Text: "Couldn't download the media."})
				return
			}
		}
		path, err := saveDownload(fileName(m, media, blob), data)
		if err != nil {
			b.emit(model.NoticeEvent{Text: "Couldn't save the file: " + err.Error()})
			return
		}
		b.emit(model.NoticeEvent{Text: "Saved to " + path})
	}()
}

// openFile opens a file with the system's default app for its type.
func openFile(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // reap the process
	return nil
}

// fileName picks a name for a saved attachment.
func fileName(m *model.Message, media model.Media, blob []byte) string {
	stamp := m.Time.Format("2006-01-02 at 15.04.05")
	switch media {
	case model.MediaDocument:
		var d waE2E.DocumentMessage
		if proto.Unmarshal(blob, &d) == nil && d.GetFileName() != "" {
			return d.GetFileName()
		}
		return "WhatsApp Document " + stamp
	case model.MediaVideo, model.MediaGIF:
		return "WhatsApp Video " + stamp + ".mp4"
	case model.MediaVoice:
		return "WhatsApp Voice " + stamp + ".ogg"
	case model.MediaAudio:
		return "WhatsApp Audio " + stamp + mediaExt(m)
	case model.MediaSticker:
		return "WhatsApp Sticker " + stamp + ".webp"
	}
	return "WhatsApp Image " + stamp + ".jpg"
}

// saveDownload writes data into the Downloads folder without overwriting.
func saveDownload(name string, data []byte) (string, error) {
	f, err := createDownload(name)
	if err != nil {
		return "", err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return f.Name(), err
}

// createDownload creates a new file in the Downloads folder, numbering the
// name ("file (1).txt") rather than overwriting one.
func createDownload(name string) (*os.File, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '_'
		}
		return r
	}, name)
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	path := filepath.Join(dir, name)
	for i := 1; ; i++ {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) && i < 1000 {
			path = filepath.Join(dir, base+" ("+itoa(i)+")"+ext)
			continue
		}
		return f, err
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var s []byte
	for ; n > 0; n /= 10 {
		s = append([]byte{byte('0' + n%10)}, s...)
	}
	return string(s)
}

// lastKey returns the timestamp and key of a chat's newest message, which
// archive, read and delete patches refer to.
func (b *Backend) lastKey(chatID string) (time.Time, *waCommon.MessageKey) {
	rc, ok := b.store.chat(b.ctx, chatID)
	if !ok || rc.last == nil {
		return time.Time{}, nil
	}
	_, key := b.messageKey(rc.last.Message)
	return rc.last.Time, key
}

// chatAction applies a chat setting locally, shows it, and syncs it.
func (b *Backend) chatAction(chatID, field string, v any, patch func(types.JID) appstate.PatchInfo) {
	jid, err := types.ParseJID(chatID)
	if err != nil {
		return
	}
	if err := b.store.setField(b.ctx, chatID, field, v); err != nil {
		b.log.Warnf("set %s of %s: %v", field, chatID, err)
	}
	b.emitChat(chatID)
	if patch != nil && b.connected() != nil {
		b.sendAppState(patch(jid))
	}
}

func (b *Backend) SetArchived(chatID string, archived bool) {
	ts, key := b.lastKey(chatID)
	if archived {
		_ = b.store.setField(b.ctx, chatID, "pinned", 0) // archiving unpins
	}
	b.chatAction(chatID, "archived", boolInt(archived), func(j types.JID) appstate.PatchInfo {
		return appstate.BuildArchive(j, archived, ts, key)
	})
}

func (b *Backend) SetMuted(chatID string, muted bool, d time.Duration) {
	v := int64(0)
	var end *int64
	switch {
	case muted && d > 0:
		v = time.Now().Add(d).Unix()
		end = proto.Int64(v * 1000) // milliseconds
	case muted:
		v = -1
	}
	b.chatAction(chatID, "muted_until", v, func(j types.JID) appstate.PatchInfo {
		return appstate.BuildMuteAbs(j, muted, end)
	})
}

func (b *Backend) SetPinned(chatID string, pinned bool) {
	v := int64(0)
	if pinned {
		v = time.Now().Unix()
	}
	b.chatAction(chatID, "pinned", v, func(j types.JID) appstate.PatchInfo {
		return appstate.BuildPin(j, pinned)
	})
}

func (b *Backend) SetUnread(chatID string, unread bool) {
	v := 0
	if unread {
		v = -1
	}
	ts, key := b.lastKey(chatID)
	b.chatAction(chatID, "unread", v, func(j types.JID) appstate.PatchInfo {
		return appstate.BuildMarkChatAsRead(j, !unread, ts, key)
	})
}

// SetFavorite implements model.Backend. Favourites sync through the
// predefined Favourites list when the phone has created it.
func (b *Backend) SetFavorite(chatID string, favorite bool) {
	fav := b.store.meta(b.ctx, "favorites_list")
	var patch func(types.JID) appstate.PatchInfo
	if fav != "" {
		patch = func(j types.JID) appstate.PatchInfo { return appstate.BuildLabelChat(j, fav, favorite) }
	}
	b.chatAction(chatID, "favorite", boolInt(favorite), patch)
}

// Lists implements model.Backend.
func (b *Backend) Lists() []*model.ChatList {
	ls, err := b.store.lists(b.ctx)
	if err != nil {
		b.log.Warnf("load lists: %v", err)
	}
	return ls
}

func (b *Backend) SetInList(chatID, listID string, in bool) {
	if err := b.store.setInList(b.ctx, listID, chatID, in); err != nil {
		b.log.Warnf("set list of %s: %v", chatID, err)
	}
	jid, err := types.ParseJID(chatID)
	if err == nil && b.connected() != nil {
		b.sendAppState(appstate.BuildLabelChat(jid, listID, in))
	}
	b.emitChat(chatID)
}

// ClearChat implements model.Backend.
func (b *Backend) ClearChat(chatID string) {
	jid, err := types.ParseJID(chatID)
	if err != nil {
		return
	}
	ts, key := b.lastKey(chatID)
	_ = b.store.clearChat(b.ctx, chatID, 0)
	b.emit(model.DeletedEvent{ChatID: chatID})
	b.emitChat(chatID)
	if b.connected() == nil {
		return
	}
	if ts.IsZero() {
		ts = time.Now()
	}
	b.sendAppState(appstate.PatchInfo{
		Type: appstate.WAPatchRegularHigh,
		Mutations: []appstate.MutationInfo{{
			Index:   []string{appstate.IndexClearChat, jid.String(), "1", "0"},
			Version: 6,
			Value: &waSyncAction.SyncActionValue{ClearChatAction: &waSyncAction.ClearChatAction{
				MessageRange: &waSyncAction.SyncActionMessageRange{
					LastMessageTimestamp: proto.Int64(ts.Unix()),
					Messages:             messageRange(key, ts),
				},
			}},
		}},
	})
}

func messageRange(key *waCommon.MessageKey, ts time.Time) []*waSyncAction.SyncActionMessage {
	if key == nil {
		return nil
	}
	return []*waSyncAction.SyncActionMessage{{Key: key, Timestamp: proto.Int64(ts.Unix())}}
}

// DeleteChat implements model.Backend.
func (b *Backend) DeleteChat(chatID string) {
	jid, err := types.ParseJID(chatID)
	if err != nil {
		return
	}
	ts, key := b.lastKey(chatID)
	_ = b.store.deleteChat(b.ctx, chatID, 0)
	b.emitAllChats()
	if b.connected() != nil {
		b.sendAppState(appstate.BuildDeleteChat(jid, ts, key, true))
	}
}

// LeaveGroup implements model.Backend.
func (b *Backend) LeaveGroup(chatID string) {
	cli := b.connected()
	jid, err := types.ParseJID(chatID)
	if cli == nil || err != nil {
		return
	}
	go func() {
		if err := cli.LeaveGroup(b.ctx, jid); err != nil {
			b.log.Warnf("leave %s: %v", chatID, err)
			b.emit(model.NoticeEvent{Text: "Couldn't exit the group."})
			return
		}
		b.emit(model.NoticeEvent{Text: "You exited the group."})
	}()
}

func (b *Backend) Pref(key string) string { return b.store.meta(b.ctx, "pref:"+key) }

func (b *Backend) SetPref(key, value string) {
	_ = b.store.setMetaValue(b.ctx, "pref:"+key, value)
	if key == model.PrefGhost {
		if value == "on" {
			b.ReportTyping("")
		}
		if cli := b.client(); cli != nil && cli.IsConnected() {
			go b.sendPresence(cli)
		}
	}
}

// Store helpers for the actions above and their app state counterparts.

func (s *msgStore) setMessageFlag(ctx context.Context, chat, id, field string, v bool) error {
	// field is always a constant from this package.
	_, err := s.db.ExecContext(ctx, `UPDATE wz_messages SET `+field+` = ? WHERE chat = ? AND id = ?`, boolInt(v), chat, id)
	return err
}

func (s *msgStore) deleteMessage(ctx context.Context, chat, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM wz_messages WHERE chat = ?1 AND id = ?2;
		DELETE FROM wz_edits WHERE chat = ?1 AND id = ?2;
		DELETE FROM wz_receipts WHERE chat = ?1 AND id = ?2;
		DELETE FROM wz_votes WHERE chat = ?1 AND id = ?2;
		DELETE FROM wz_reactions WHERE chat = ?1 AND id = ?2`, chat, id)
	return err
}

// dropOrphanEdits deletes the versions, receipts, votes and reactions of
// chat ?1's messages that are gone.
const dropOrphanEdits = `DELETE FROM wz_edits WHERE chat = ?1 AND NOT EXISTS
	(SELECT 1 FROM wz_messages m WHERE m.chat = wz_edits.chat AND m.id = wz_edits.id);
	DELETE FROM wz_receipts WHERE chat = ?1 AND NOT EXISTS
	(SELECT 1 FROM wz_messages m WHERE m.chat = wz_receipts.chat AND m.id = wz_receipts.id);
	DELETE FROM wz_votes WHERE chat = ?1 AND NOT EXISTS
	(SELECT 1 FROM wz_messages m WHERE m.chat = wz_votes.chat AND m.id = wz_votes.id);
	DELETE FROM wz_reactions WHERE chat = ?1 AND NOT EXISTS
	(SELECT 1 FROM wz_messages m WHERE m.chat = wz_reactions.chat AND m.id = wz_reactions.id)`

// clearChat deletes the chat's unstarred messages sent at or before upTo
// (unix seconds), or all of them when upTo is 0.
func (s *msgStore) clearChat(ctx context.Context, chat string, upTo int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM wz_messages WHERE chat = ?1 AND starred = 0 AND (?2 = 0 OR ts <= ?2);
		`+dropOrphanEdits, chat, upTo)
	return err
}

// deleteChat deletes the chat's messages sent at or before upTo (unix
// seconds), or all of them when upTo is 0, and then the chat itself unless
// newer messages are left.
func (s *msgStore) deleteChat(ctx context.Context, chat string, upTo int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM wz_messages WHERE chat = ?1 AND (?2 = 0 OR ts <= ?2);
		DELETE FROM wz_chats WHERE jid = ?1 AND NOT EXISTS (SELECT 1 FROM wz_messages WHERE chat = ?1);
		DELETE FROM wz_list_chats WHERE chat = ?1 AND NOT EXISTS (SELECT 1 FROM wz_chats WHERE jid = ?1);
		`+dropOrphanEdits, chat, upTo)
	return err
}

func (s *msgStore) putList(ctx context.Context, id, name string, custom, deleted bool, order int) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO wz_lists (id, name, custom, ord, deleted) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET name = excluded.name, custom = excluded.custom, ord = excluded.ord,
			deleted = excluded.deleted`, id, name, boolInt(custom), order, boolInt(deleted))
	return err
}

func (s *msgStore) setInList(ctx context.Context, list, chat string, in bool) error {
	q := `INSERT OR IGNORE INTO wz_list_chats (list, chat) VALUES (?, ?)`
	if !in {
		q = `DELETE FROM wz_list_chats WHERE list = ? AND chat = ?`
	}
	_, err := s.db.ExecContext(ctx, q, list, chat)
	return err
}

func (s *msgStore) lists(ctx context.Context) ([]*model.ChatList, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT l.id, l.name, COALESCE(c.chat, '') FROM wz_lists l
		LEFT JOIN wz_list_chats c ON c.list = l.id
		WHERE l.custom = 1 AND l.deleted = 0 AND l.name <> '' ORDER BY l.ord, l.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.ChatList
	for rows.Next() {
		var id, name, chat string
		if err := rows.Scan(&id, &name, &chat); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].ID != id {
			out = append(out, &model.ChatList{ID: id, Name: name})
		}
		if chat != "" {
			l := out[len(out)-1]
			l.Chats = append(l.Chats, chat)
		}
	}
	return out, rows.Err()
}

// App state events from the phone for the same actions.

func (b *Backend) onLabelEdit(id string, a *waSyncAction.LabelEditAction) {
	ctx := b.ctx
	if a.GetType() == waSyncAction.LabelEditAction_FAVORITES {
		_ = b.store.setMetaValue(ctx, "favorites_list", id)
	}
	custom := a.GetType() == waSyncAction.LabelEditAction_CUSTOM || a.GetType() == waSyncAction.LabelEditAction_NONE
	if err := b.store.putList(ctx, id, a.GetName(), custom, a.GetDeleted(), int(a.GetOrderIndex())); err != nil {
		b.log.Warnf("store list %s: %v", id, err)
	}
}

func (b *Backend) onLabelChat(j types.JID, list string, labeled, quiet bool) {
	ctx := b.ctx
	chat := b.canonical(ctx, j).String()
	_ = b.store.setInList(ctx, list, chat, labeled)
	if list == b.store.meta(ctx, "favorites_list") {
		_ = b.store.setField(ctx, chat, "favorite", boolInt(labeled))
	}
	if !quiet {
		b.emitChat(chat)
	}
}

// onFavorites applies the phone's full list of favourite chats.
func (b *Backend) onFavorites(a *waSyncAction.FavoritesAction) {
	ctx := b.ctx
	_, _ = b.db.ExecContext(ctx, `UPDATE wz_chats SET favorite = 0`)
	for _, f := range a.GetFavorites() {
		if j, err := types.ParseJID(f.GetID()); err == nil {
			_ = b.store.setField(ctx, b.canonical(ctx, j).String(), "favorite", 1)
		}
	}
	b.emitAllChats()
}
