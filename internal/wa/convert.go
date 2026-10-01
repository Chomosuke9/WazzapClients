package wa

import (
	"context"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waWeb"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// parsed is the result of interpreting one incoming message. Most messages
// become a new chat entry; reactions, deletions and edits modify an existing one.
type parsed struct {
	msg storedMsg

	target   string // message ID a reaction/revoke/edit/pin applies to
	reaction string
	revoke   bool
	edit     string
	pin      int // 1 pinned, -1 unpinned
}

// content summarizes what a message shows.
type content struct {
	text     string
	kind     model.Kind
	media    model.Media
	duration int
	thumb    []byte
	blob     []byte // marshaled media message, for downloads
	bg       uint32 // ARGB background of a text status
	ctx      *waE2E.ContextInfo
	buttons  *buttonsInfo // footer and buttons of business messages
	file     fileInfo     // documents and audio
}

func marshal(m proto.Message) []byte {
	b, _ := proto.Marshal(m)
	return b
}

// unwrap strips the envelopes a message can come in (disappearing,
// view once, edited...). Quoted messages keep them, unlike the message
// events hypermeow hands out.
func unwrap(m *waE2E.Message) *waE2E.Message {
	for i := 0; i < 4 && m != nil; i++ {
		var inner *waE2E.Message
		switch {
		case m.GetDeviceSentMessage().GetMessage() != nil:
			inner = m.GetDeviceSentMessage().GetMessage()
		case m.GetEphemeralMessage().GetMessage() != nil:
			inner = m.GetEphemeralMessage().GetMessage()
		case m.GetViewOnceMessage().GetMessage() != nil:
			inner = m.GetViewOnceMessage().GetMessage()
		case m.GetViewOnceMessageV2().GetMessage() != nil:
			inner = m.GetViewOnceMessageV2().GetMessage()
		case m.GetViewOnceMessageV2Extension().GetMessage() != nil:
			inner = m.GetViewOnceMessageV2Extension().GetMessage()
		case m.GetDocumentWithCaptionMessage().GetMessage() != nil:
			inner = m.GetDocumentWithCaptionMessage().GetMessage()
		case m.GetEditedMessage().GetMessage() != nil:
			inner = m.GetEditedMessage().GetMessage()
		case m.GetBotInvokeMessage().GetMessage() != nil:
			inner = m.GetBotInvokeMessage().GetMessage()
		case m.GetLottieStickerMessage().GetMessage() != nil:
			inner = m.GetLottieStickerMessage().GetMessage()
		case m.GetAssociatedChildMessage().GetMessage() != nil:
			inner = m.GetAssociatedChildMessage().GetMessage()
		case m.GetGroupMentionedMessage().GetMessage() != nil:
			inner = m.GetGroupMentionedMessage().GetMessage()
		default:
			return m
		}
		m = inner
	}
	return m
}

func describe(m *waE2E.Message) content {
	m = unwrap(m)
	switch {
	case m.GetConversation() != "":
		return content{text: m.GetConversation()}
	case m.GetExtendedTextMessage() != nil:
		e := m.GetExtendedTextMessage()
		return content{text: e.GetText(), bg: e.GetBackgroundArgb(), ctx: e.GetContextInfo()}
	case m.GetImageMessage() != nil:
		e := m.GetImageMessage()
		return content{text: e.GetCaption(), kind: model.KindImage, media: model.MediaImage,
			thumb: e.GetJPEGThumbnail(), blob: marshal(e), ctx: e.GetContextInfo()}
	case m.GetVideoMessage() != nil:
		e := m.GetVideoMessage()
		media := model.MediaVideo
		if e.GetGifPlayback() {
			media = model.MediaGIF
		}
		return content{text: e.GetCaption(), kind: model.KindImage, media: media, duration: int(e.GetSeconds()),
			thumb: e.GetJPEGThumbnail(), blob: marshal(e), ctx: e.GetContextInfo()}
	case m.GetPtvMessage() != nil:
		e := m.GetPtvMessage()
		return content{kind: model.KindImage, media: model.MediaVideo, duration: int(e.GetSeconds()),
			thumb: e.GetJPEGThumbnail(), blob: marshal(e), ctx: e.GetContextInfo()}
	case m.GetAudioMessage() != nil:
		e := m.GetAudioMessage()
		media := model.MediaAudio
		if e.GetPTT() {
			media = model.MediaVoice
		}
		return content{media: media, duration: int(e.GetSeconds()), blob: marshal(e), ctx: e.GetContextInfo(),
			file: audioInfo(e)}
	case m.GetDocumentMessage() != nil:
		e := m.GetDocumentMessage()
		f := documentInfo(e)
		return content{text: first(e.GetCaption(), f.Name), media: model.MediaDocument, blob: marshal(e),
			ctx: e.GetContextInfo(), file: f}
	case m.GetStickerMessage() != nil:
		e := m.GetStickerMessage()
		return content{kind: model.KindSticker, media: model.MediaSticker, thumb: e.GetPngThumbnail(),
			blob: marshal(e), ctx: e.GetContextInfo()}
	case m.GetContactMessage() != nil:
		e := m.GetContactMessage()
		return content{text: e.GetDisplayName(), media: model.MediaContact, ctx: e.GetContextInfo()}
	case m.GetContactsArrayMessage() != nil:
		e := m.GetContactsArrayMessage()
		return content{text: e.GetDisplayName(), media: model.MediaContact, ctx: e.GetContextInfo()}
	case m.GetLocationMessage() != nil:
		e := m.GetLocationMessage()
		return content{text: e.GetName(), media: model.MediaLocation, thumb: e.GetJPEGThumbnail(), ctx: e.GetContextInfo()}
	case m.GetLiveLocationMessage() != nil:
		e := m.GetLiveLocationMessage()
		return content{text: "Live location", media: model.MediaLocation, ctx: e.GetContextInfo()}
	case m.GetPollCreationMessage() != nil:
		e := m.GetPollCreationMessage()
		return content{text: e.GetName(), media: model.MediaPoll, ctx: e.GetContextInfo()}
	case m.GetPollCreationMessageV2() != nil:
		e := m.GetPollCreationMessageV2()
		return content{text: e.GetName(), media: model.MediaPoll, ctx: e.GetContextInfo()}
	case m.GetPollCreationMessageV3() != nil:
		e := m.GetPollCreationMessageV3()
		return content{text: e.GetName(), media: model.MediaPoll, ctx: e.GetContextInfo()}
	// Answers to bot buttons and lists quote the message they answer.
	case m.GetButtonsResponseMessage() != nil:
		e := m.GetButtonsResponseMessage()
		return content{text: e.GetSelectedDisplayText(), ctx: e.GetContextInfo()}
	case m.GetTemplateButtonReplyMessage() != nil:
		e := m.GetTemplateButtonReplyMessage()
		return content{text: e.GetSelectedDisplayText(), ctx: e.GetContextInfo()}
	case m.GetListResponseMessage() != nil:
		e := m.GetListResponseMessage()
		return content{text: first(e.GetTitle(), e.GetSingleSelectReply().GetSelectedRowID()), ctx: e.GetContextInfo()}
	case m.GetInteractiveResponseMessage() != nil:
		e := m.GetInteractiveResponseMessage()
		return content{text: e.GetBody().GetText(), ctx: e.GetContextInfo()}
	case m.GetButtonsMessage() != nil:
		return describeButtons(m.GetButtonsMessage())
	case m.GetTemplateMessage() != nil:
		return describeTemplate(m.GetTemplateMessage())
	case m.GetInteractiveMessage() != nil:
		return describeInteractive(m.GetInteractiveMessage())
	case m.GetListMessage() != nil:
		return describeList(m.GetListMessage())
	case m.GetGroupInviteMessage() != nil:
		e := m.GetGroupInviteMessage()
		return content{text: "Group invite: " + e.GetGroupName(), ctx: e.GetContextInfo()}
	}
	return content{}
}

func webReceipt(w *waWeb.WebMessageInfo) model.Receipt {
	switch w.GetStatus() {
	case waWeb.WebMessageInfo_READ, waWeb.WebMessageInfo_PLAYED:
		return model.Read
	case waWeb.WebMessageInfo_DELIVERY_ACK:
		return model.Delivered
	}
	return model.Sent
}

// parse interprets a message event. ok is false for messages with nothing to
// show (key distribution, unsupported types, and so on). It may query the
// device store, so never call it inside a msgStore transaction.
func (b *Backend) parse(ctx context.Context, evt *events.Message) (p parsed, ok bool) {
	m := evt.Message
	if m == nil {
		return p, false
	}
	chat := b.canonical(ctx, evt.Info.Chat)
	if skipChat(chat) {
		return p, false
	}
	p.msg.Message = &model.Message{ChatID: chat.String()}

	if r := m.GetReactionMessage(); r != nil {
		p.target, p.reaction = r.GetKey().GetID(), r.GetText()
		return p, p.target != ""
	}
	if pin := m.GetPinInChatMessage(); pin != nil {
		p.target, p.pin = pin.GetKey().GetID(), 1
		if pin.GetType() == waE2E.PinInChatMessage_UNPIN_FOR_ALL {
			p.pin = -1
		}
		return p, p.target != ""
	}
	if pm := m.GetProtocolMessage(); pm != nil {
		p.target = pm.GetKey().GetID()
		switch pm.GetType() {
		case waE2E.ProtocolMessage_REVOKE:
			p.revoke = true
		case waE2E.ProtocolMessage_MESSAGE_EDIT:
			p.edit = describe(pm.GetEditedMessage()).text
			if p.edit == "" {
				return p, false
			}
		default:
			return p, false
		}
		return p, p.target != ""
	}

	c := describe(m)
	if c.text == "" && c.media == model.MediaNone && c.buttons.empty() {
		if !hasContent(m) {
			return p, false
		}
		// Something the app can't show: say so instead of dropping it.
		b.log.Infof("unsupported message %s in %s: %s", evt.Info.ID, chat, fieldNames(m))
		c = content{kind: model.KindUnsupported}
	}
	msg := p.msg.Message
	msg.ID = evt.Info.ID
	msg.Kind = c.kind
	msg.Media = c.media
	msg.Duration = c.duration
	msg.FromMe = evt.Info.IsFromMe
	msg.Text = c.text
	msg.Time = evt.Info.Timestamp
	msg.Thumb = c.thumb
	c.file.apply(msg)
	msg.Receipt = model.Sent
	if evt.SourceWebMsg != nil && msg.FromMe {
		msg.Receipt = webReceipt(evt.SourceWebMsg)
	}
	p.msg.senderJID = evt.Info.Sender.ToNonAD().String()
	p.msg.senderPush = evt.Info.PushName
	p.msg.mediaBlob = c.blob
	if !c.buttons.empty() {
		p.msg.buttons = c.buttons
		c.buttons.apply(msg)
	}
	p.msg.mentions = append([]string(nil), c.ctx.GetMentionedJID()...)
	if c.ctx.GetNonJIDMentions() > 0 {
		p.msg.mentions = append(p.msg.mentions, mentionAll)
	}
	for _, gm := range c.ctx.GetGroupMentions() {
		p.msg.mentions = append(p.msg.mentions, groupMention(gm.GetGroupJID(), gm.GetGroupSubject()))
	}
	msg.Forwarded = c.ctx.GetIsForwarded()
	b.parseQuote(ctx, &p.msg, c.ctx)
	return p, true
}

// parseQuote fills in the message a reply points to. The quoted content
// normally travels with the reply; when it is missing (or of a type that
// isn't rendered) the original is looked up by its ID.
func (b *Backend) parseQuote(ctx context.Context, m *storedMsg, ci *waE2E.ContextInfo) {
	id := ci.GetStanzaID()
	q := ci.GetQuotedMessage()
	if id == "" && q == nil {
		return
	}
	qc := describe(q)
	quote := &model.Quote{ID: id, Text: qc.text, Media: qc.media}
	m.quoteJID = ci.GetParticipant()
	if qc.text == "" && qc.media == model.MediaNone && id != "" {
		orig, ok := b.store.message(ctx, m.ChatID, id)
		switch {
		case ok:
			quote.Text, quote.Media = orig.Text, orig.Media
			switch orig.Kind {
			case model.KindDeleted:
				quote.Text = "This message was deleted"
			case model.KindUnsupported:
				quote.Text = "This message couldn't load"
			}
			if m.quoteJID == "" {
				m.quoteJID = orig.senderJID
				if orig.FromMe {
					m.quoteJID = b.ownJID(m.ChatID).String()
				}
			}
		case q == nil:
			return // only an ID, of a message we don't have
		}
	}
	if j, err := types.ParseJID(m.quoteJID); err == nil && !j.IsEmpty() {
		m.quoteJID = b.canonical(ctx, j).String()
	}
	m.quoteID = id
	m.Quote = quote
}
