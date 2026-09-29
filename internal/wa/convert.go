package wa

import (
	"context"
	"fmt"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waWeb"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// parsed is the result of interpreting one incoming message. Most messages
// become a new chat entry; reactions, deletions and edits modify an existing one.
type parsed struct {
	chat types.JID
	msg  storedMsg

	target   string // message ID a reaction/revoke/edit applies to
	reaction string
	revoke   bool
	edit     string
}

// describe summarizes message content: display text, kind, thumbnail, and
// reply context.
func describe(m *waE2E.Message) (text string, kind model.Kind, thumb []byte, ctx *waE2E.ContextInfo) {
	switch {
	case m.GetConversation() != "":
		return m.GetConversation(), model.KindText, nil, nil
	case m.GetExtendedTextMessage() != nil:
		e := m.GetExtendedTextMessage()
		return e.GetText(), model.KindText, nil, e.GetContextInfo()
	case m.GetImageMessage() != nil:
		e := m.GetImageMessage()
		return e.GetCaption(), model.KindImage, e.GetJPEGThumbnail(), e.GetContextInfo()
	case m.GetVideoMessage() != nil:
		e := m.GetVideoMessage()
		t := "🎥 Video"
		if c := e.GetCaption(); c != "" {
			t = "🎥 " + c
		}
		return t, model.KindImage, e.GetJPEGThumbnail(), e.GetContextInfo()
	case m.GetAudioMessage() != nil:
		e := m.GetAudioMessage()
		if e.GetPTT() {
			s := e.GetSeconds()
			return fmt.Sprintf("🎤 Voice message (%d:%02d)", s/60, s%60), model.KindText, nil, e.GetContextInfo()
		}
		return "🎵 Audio", model.KindText, nil, e.GetContextInfo()
	case m.GetDocumentMessage() != nil:
		e := m.GetDocumentMessage()
		t := "📄 " + e.GetFileName()
		if c := e.GetCaption(); c != "" {
			t += "\n" + c
		}
		return t, model.KindText, nil, e.GetContextInfo()
	case m.GetStickerMessage() != nil:
		return "Sticker", model.KindText, nil, m.GetStickerMessage().GetContextInfo()
	case m.GetContactMessage() != nil:
		return "👤 " + m.GetContactMessage().GetDisplayName(), model.KindText, nil, nil
	case m.GetContactsArrayMessage() != nil:
		return "👥 Contacts", model.KindText, nil, nil
	case m.GetLocationMessage() != nil:
		return "📍 Location", model.KindText, nil, m.GetLocationMessage().GetContextInfo()
	case m.GetLiveLocationMessage() != nil:
		return "📍 Live location", model.KindText, nil, nil
	case m.GetPollCreationMessage() != nil:
		return "📊 " + m.GetPollCreationMessage().GetName(), model.KindText, nil, nil
	case m.GetPollCreationMessageV2() != nil:
		return "📊 " + m.GetPollCreationMessageV2().GetName(), model.KindText, nil, nil
	case m.GetPollCreationMessageV3() != nil:
		return "📊 " + m.GetPollCreationMessageV3().GetName(), model.KindText, nil, nil
	}
	return "", model.KindText, nil, nil
}

// quoteText is a one-line summary for reply previews.
func quoteText(m *waE2E.Message) string {
	text, kind, _, _ := describe(m)
	if kind == model.KindImage && text == "" {
		return "📷 Photo"
	}
	if kind == model.KindImage && m.GetImageMessage() != nil {
		return "📷 " + text
	}
	return text
}

func webReceipt(w *waWeb.WebMessageInfo) model.Receipt {
	switch w.GetStatus() {
	case waWeb.WebMessageInfo_READ, waWeb.WebMessageInfo_PLAYED:
		return model.Read
	case waWeb.WebMessageInfo_DELIVERY_ACK:
		return model.Delivered
	case waWeb.WebMessageInfo_SERVER_ACK:
		return model.Sent
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
	p.chat = b.canonical(ctx, evt.Info.Chat)
	if skipChat(p.chat) {
		return p, false
	}

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
			p.edit, _, _, _ = describe(pm.GetEditedMessage())
			if p.edit == "" {
				return p, false
			}
		default:
			return p, false
		}
		return p, p.target != ""
	}

	text, kind, thumb, ci := describe(m)
	if text == "" && kind == model.KindText {
		return p, false
	}
	msg := &model.Message{
		ID:      evt.Info.ID,
		ChatID:  p.chat.String(),
		Kind:    kind,
		FromMe:  evt.Info.IsFromMe,
		Text:    text,
		Time:    evt.Info.Timestamp,
		Thumb:   thumb,
		Receipt: model.Sent,
	}
	if evt.SourceWebMsg != nil && msg.FromMe {
		msg.Receipt = webReceipt(evt.SourceWebMsg)
	}
	if evt.Info.IsGroup && !msg.FromMe {
		msg.Sender = b.senderName(ctx, evt.Info.Sender, evt.Info.PushName)
	}
	if q := ci.GetQuotedMessage(); q != nil {
		quote := &model.Quote{Text: quoteText(q)}
		if pj, err := types.ParseJID(ci.GetParticipant()); err == nil && !pj.IsEmpty() {
			quote.Sender = b.senderName(ctx, pj, "")
		}
		msg.Quote = quote
	}
	p.msg = storedMsg{Message: msg, senderJID: evt.Info.Sender.ToNonAD().String()}
	return p, true
}
