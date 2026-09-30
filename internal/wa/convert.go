package wa

import (
	"context"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waWeb"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// parsed is the result of interpreting one incoming message. Most messages
// become a new chat entry; reactions, deletions and edits modify an existing one.
type parsed struct {
	msg storedMsg

	target   string // message ID a reaction/revoke/edit applies to
	reaction string
	revoke   bool
	edit     string
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
}

func marshal(m proto.Message) []byte {
	b, _ := proto.Marshal(m)
	return b
}

func describe(m *waE2E.Message) content {
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
			thumb: e.GetJPEGThumbnail(), ctx: e.GetContextInfo()}
	case m.GetAudioMessage() != nil:
		e := m.GetAudioMessage()
		media := model.MediaAudio
		if e.GetPTT() {
			media = model.MediaVoice
		}
		return content{media: media, duration: int(e.GetSeconds()), ctx: e.GetContextInfo()}
	case m.GetDocumentMessage() != nil:
		e := m.GetDocumentMessage()
		text := e.GetFileName()
		if text == "" {
			text = e.GetTitle()
		}
		return content{text: text, media: model.MediaDocument, ctx: e.GetContextInfo()}
	case m.GetStickerMessage() != nil:
		e := m.GetStickerMessage()
		return content{kind: model.KindSticker, media: model.MediaSticker, thumb: e.GetPngThumbnail(),
			blob: marshal(e), ctx: e.GetContextInfo()}
	case m.GetContactMessage() != nil:
		return content{text: m.GetContactMessage().GetDisplayName(), media: model.MediaContact}
	case m.GetContactsArrayMessage() != nil:
		return content{text: m.GetContactsArrayMessage().GetDisplayName(), media: model.MediaContact}
	case m.GetLocationMessage() != nil:
		e := m.GetLocationMessage()
		return content{text: e.GetName(), media: model.MediaLocation, thumb: e.GetJPEGThumbnail(), ctx: e.GetContextInfo()}
	case m.GetLiveLocationMessage() != nil:
		return content{text: "Live location", media: model.MediaLocation}
	case m.GetPollCreationMessage() != nil:
		return content{text: m.GetPollCreationMessage().GetName(), media: model.MediaPoll}
	case m.GetPollCreationMessageV2() != nil:
		return content{text: m.GetPollCreationMessageV2().GetName(), media: model.MediaPoll}
	case m.GetPollCreationMessageV3() != nil:
		return content{text: m.GetPollCreationMessageV3().GetName(), media: model.MediaPoll}
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
	if c.text == "" && c.media == model.MediaNone {
		return p, false
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
	msg.Receipt = model.Sent
	if evt.SourceWebMsg != nil && msg.FromMe {
		msg.Receipt = webReceipt(evt.SourceWebMsg)
	}
	p.msg.senderJID = evt.Info.Sender.ToNonAD().String()
	p.msg.senderPush = evt.Info.PushName
	p.msg.mediaBlob = c.blob
	p.msg.mentions = c.ctx.GetMentionedJID()
	if q := c.ctx.GetQuotedMessage(); q != nil {
		qc := describe(q)
		msg.Quote = &model.Quote{Text: qc.text, Media: qc.media}
		p.msg.quoteJID = c.ctx.GetParticipant()
	}
	return p, true
}
