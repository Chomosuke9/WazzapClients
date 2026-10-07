package wa

import (
	"encoding/json"
	"strings"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Business and bot messages come with buttons. Only quick replies, links
// and copy-code buttons are supported; other buttons are left out.

// Where a button came from decides how pressing it is answered.
const (
	srcButtons  = "buttons"  // ButtonsMessage
	srcTemplate = "template" // TemplateMessage (hydrated four-row template)
	srcNative   = "native"   // native flow button (InteractiveMessage)
)

// buttonsInfo is a message's footer and the buttons the app supports.
type buttonsInfo struct {
	Footer  string
	Buttons []storedButton
}

// storedButton is a button and where it came from, to answer it.
type storedButton struct {
	model.Button
	Source string
	Index  int
}

func (bi *buttonsInfo) empty() bool { return bi == nil || (bi.Footer == "" && len(bi.Buttons) == 0) }

func (bi *buttonsInfo) add(src string, i int, b model.Button) {
	if b.Label == "" && b.Kind == model.ButtonCopy {
		b.Label = "Copy code"
	}
	if b.Label == "" || (b.Kind != model.ButtonReply && b.Value == "") {
		return
	}
	bi.Buttons = append(bi.Buttons, storedButton{Button: b, Source: src, Index: i})
}

// apply copies the footer and buttons into a model message.
func (bi *buttonsInfo) apply(m *model.Message) {
	if bi == nil {
		return
	}
	m.Footer = bi.Footer
	m.Buttons = nil
	for _, b := range bi.Buttons {
		m.Buttons = append(m.Buttons, b.Button)
	}
}

// headerMedia describes a business message's picture, video or document
// header, if it has one.
func headerMedia(img *waE2E.ImageMessage, vid *waE2E.VideoMessage, doc *waE2E.DocumentMessage) content {
	switch {
	case img != nil:
		return describe(&waE2E.Message{ImageMessage: img})
	case vid != nil:
		return describe(&waE2E.Message{VideoMessage: vid})
	case doc != nil:
		return describe(&waE2E.Message{DocumentMessage: doc})
	}
	return content{}
}

// titled puts a bold title line above a message's text.
func titled(title, body string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return body
	}
	if body == "" {
		return "*" + title + "*"
	}
	return "*" + title + "*\n" + body
}

func describeButtons(e *waE2E.ButtonsMessage) content {
	c := headerMedia(e.GetImageMessage(), e.GetVideoMessage(), e.GetDocumentMessage())
	c.text = titled(e.GetText(), e.GetContentText())
	c.ctx = e.GetContextInfo()
	bi := &buttonsInfo{Footer: e.GetFooterText()}
	for i, bt := range e.GetButtons() {
		if bt.GetType() == waE2E.ButtonsMessage_Button_NATIVE_FLOW {
			if b, ok := nativeButton(bt.GetNativeFlowInfo().GetName(), bt.GetNativeFlowInfo().GetParamsJSON()); ok {
				bi.add(srcNative, i, b)
			}
			continue
		}
		bi.add(srcButtons, i, model.Button{Kind: model.ButtonReply, Label: bt.GetButtonText().GetDisplayText(), Value: bt.GetButtonID()})
	}
	c.buttons = bi
	return c
}

func describeTemplate(e *waE2E.TemplateMessage) content {
	if im := e.GetInteractiveMessageTemplate(); im != nil {
		c := describeInteractive(im)
		if c.ctx == nil {
			c.ctx = e.GetContextInfo()
		}
		return c
	}
	t := e.GetHydratedTemplate()
	if t == nil {
		t = e.GetHydratedFourRowTemplate()
	}
	if t == nil {
		return content{}
	}
	c := headerMedia(t.GetImageMessage(), t.GetVideoMessage(), t.GetDocumentMessage())
	c.text = titled(t.GetHydratedTitleText(), t.GetHydratedContentText())
	c.ctx = e.GetContextInfo()
	bi := &buttonsInfo{Footer: t.GetHydratedFooterText()}
	for i, hb := range t.GetHydratedButtons() {
		idx := i
		if hb.Index != nil {
			idx = int(hb.GetIndex())
		}
		switch {
		case hb.GetQuickReplyButton() != nil:
			q := hb.GetQuickReplyButton()
			bi.add(srcTemplate, idx, model.Button{Kind: model.ButtonReply, Label: q.GetDisplayText(), Value: q.GetID()})
		case hb.GetUrlButton() != nil:
			u := hb.GetUrlButton()
			bi.add(srcTemplate, idx, model.Button{Kind: model.ButtonURL, Label: u.GetDisplayText(), Value: u.GetURL()})
		}
	}
	c.buttons = bi
	return c
}

func describeInteractive(e *waE2E.InteractiveMessage) content {
	h := e.GetHeader()
	c := headerMedia(h.GetImageMessage(), h.GetVideoMessage(), h.GetDocumentMessage())
	if c.kind == model.KindImage && len(c.thumb) == 0 {
		c.thumb = h.GetJPEGThumbnail()
	}
	c.text = titled(h.GetTitle(), e.GetBody().GetText())
	c.ctx = e.GetContextInfo()
	bi := &buttonsInfo{Footer: e.GetFooter().GetText()}
	for i, nb := range e.GetNativeFlowMessage().GetButtons() {
		if b, ok := nativeButton(nb.GetName(), nb.GetButtonParamsJSON()); ok {
			bi.add(srcNative, i, b)
		}
	}
	c.buttons = bi
	return c
}

func describeList(e *waE2E.ListMessage) content {
	return content{text: titled(e.GetTitle(), e.GetDescription()), ctx: e.GetContextInfo(),
		buttons: &buttonsInfo{Footer: e.GetFooterText()}}
}

// nativeButton reads a native flow button's JSON parameters.
func nativeButton(name, params string) (model.Button, bool) {
	var p struct {
		DisplayText string `json:"display_text"`
		ID          string `json:"id"`
		URL         string `json:"url"`
		CopyCode    string `json:"copy_code"`
	}
	if json.Unmarshal([]byte(params), &p) != nil {
		return model.Button{}, false
	}
	switch name {
	case "quick_reply":
		return model.Button{Kind: model.ButtonReply, Label: p.DisplayText, Value: p.ID}, true
	case "cta_url":
		return model.Button{Kind: model.ButtonURL, Label: p.DisplayText, Value: p.URL}, true
	case "cta_copy":
		return model.Button{Kind: model.ButtonCopy, Label: p.DisplayText, Value: p.CopyCode}, true
	}
	return model.Button{}, false
}

// Message fields that carry nothing to show in a chat. A message with any
// other field that describe doesn't understand is shown as "couldn't load".
var noContentFields = map[protoreflect.Name]bool{
	"conversation":                               true, // empty text
	"senderKeyDistributionMessage":               true,
	"fastRatchetKeySenderKeyDistributionMessage": true,
	"messageContextInfo":                         true,
	"protocolMessage":                            true,
	"reactionMessage":                            true,
	"encReactionMessage":                         true,
	"pollUpdateMessage":                          true,
	"keepInChatMessage":                          true,
	"pinInChatMessage":                           true,
	"call":                                       true,
	"chat":                                       true,
	"peerDataOperationRequestMessage":            true,
	"peerDataOperationRequestResponseMessage":    true,
	"encCommentMessage":                          true,
	"commentMessage":                             true,
	"encEventResponseMessage":                    true,
	"secretEncryptedMessage":                     true,
	"albumMessage":                               true, // its pictures arrive as their own messages
	"stickerSyncRmrMessage":                      true,
	"messageHistoryBundle":                       true,
	"messageHistoryNotice":                       true,
	"limitSharingMessage":                        true,
	"appStateSyncKeyShare":                       true,
}

// hasContent reports whether a message has something a person would see,
// even if the app can't show it.
func hasContent(m *waE2E.Message) bool {
	m = unwrap(m)
	if m == nil {
		return false
	}
	found := false
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		found = !noContentFields[fd.Name()]
		return !found
	})
	return found
}

// fieldNames lists the fields a message has, after unwrapping, for logs.
func fieldNames(m *waE2E.Message) string {
	var names []string
	unwrap(m).ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		names = append(names, string(fd.Name()))
		return true
	})
	return strings.Join(names, ", ")
}

// PressButton implements model.Backend.
func (b *Backend) PressButton(m *model.Message, i int) *model.Message {
	cli := b.connected()
	jid, err := types.ParseJID(m.ChatID)
	if cli == nil || err != nil {
		return nil
	}
	ctx := b.ctx
	raw, ok := b.store.message(ctx, m.ChatID, m.ID)
	if !ok || raw.buttons == nil || i < 0 || i >= len(raw.buttons.Buttons) {
		return nil
	}
	bt := raw.buttons.Buttons[i]
	if bt.Kind != model.ButtonReply {
		return nil
	}
	sender := b.senderOf(m)
	ci := &waE2E.ContextInfo{
		StanzaID:      proto.String(m.ID),
		Participant:   proto.String(sender.String()),
		QuotedMessage: b.quotedMessage(ctx, m.ChatID, m.ID),
	}
	var msg *waE2E.Message
	switch bt.Source {
	case srcTemplate:
		msg = &waE2E.Message{TemplateButtonReplyMessage: &waE2E.TemplateButtonReplyMessage{
			SelectedID:          proto.String(bt.Value),
			SelectedDisplayText: proto.String(bt.Label),
			SelectedIndex:       proto.Uint32(uint32(bt.Index)),
			ContextInfo:         ci,
		}}
	case srcNative:
		params, _ := json.Marshal(map[string]string{"id": bt.Value})
		msg = &waE2E.Message{InteractiveResponseMessage: &waE2E.InteractiveResponseMessage{
			Body: &waE2E.InteractiveResponseMessage_Body{
				Text:   proto.String(bt.Label),
				Format: waE2E.InteractiveResponseMessage_Body_DEFAULT.Enum(),
			},
			InteractiveResponseMessage: &waE2E.InteractiveResponseMessage_NativeFlowResponseMessage_{
				NativeFlowResponseMessage: &waE2E.InteractiveResponseMessage_NativeFlowResponseMessage{
					Name:       proto.String("quick_reply"),
					ParamsJSON: proto.String(string(params)),
					Version:    proto.Int32(3),
				},
			},
			ContextInfo: ci,
		}}
	default:
		msg = &waE2E.Message{ButtonsResponseMessage: &waE2E.ButtonsResponseMessage{
			SelectedButtonID: proto.String(bt.Value),
			Response:         &waE2E.ButtonsResponseMessage_SelectedDisplayText{SelectedDisplayText: bt.Label},
			Type:             waE2E.ButtonsResponseMessage_DISPLAY_TEXT.Enum(),
			ContextInfo:      ci,
		}}
	}
	out := &model.Message{
		ID:      cli.GenerateMessageID(),
		ChatID:  m.ChatID,
		FromMe:  true,
		Text:    bt.Label,
		Time:    b.sendTime(),
		Receipt: model.Pending,
		Quote:   &model.Quote{ID: m.ID, SenderID: sender.String(), Text: raw.Text, Media: raw.Media},
	}
	// The server may refuse button answers (error 479 was seen for native
	// flow quick replies in groups); a quoted text reply still gets the
	// answer across.
	fallback := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text:        proto.String(bt.Label),
		ContextInfo: ci,
	}}
	return b.storeAndSend(jid, storedMsg{Message: out}, msg, fallback)
}
