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

// Business and bot messages come with buttons. Only quick replies, lists,
// links and copy-code buttons are supported; other buttons are left out.

// Where a button came from decides how pressing it is answered.
const (
	srcButtons  = "buttons"  // ButtonsMessage
	srcTemplate = "template" // TemplateMessage (hydrated four-row template)
	srcNative   = "native"   // native flow button (InteractiveMessage)
	srcList     = "list"     // ListMessage's button
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
	switch {
	case b.Label == "":
		return
	case b.Kind == model.ButtonList:
		if len(b.Sections) == 0 {
			return
		}
	case b.Kind != model.ButtonReply && b.Value == "":
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
	bi := &buttonsInfo{Footer: e.GetFooterText()}
	var sections []model.ListSection
	for _, sec := range e.GetSections() {
		var rows []model.ListRow
		for _, r := range sec.GetRows() {
			rows = append(rows, model.ListRow{ID: r.GetRowID(), Title: r.GetTitle(), Description: r.GetDescription()})
		}
		sections = appendSection(sections, sec.GetTitle(), rows)
	}
	bi.add(srcList, 0, model.Button{Kind: model.ButtonList, Label: first(e.GetButtonText(), "Select"), Sections: sections})
	return content{text: titled(e.GetTitle(), e.GetDescription()), ctx: e.GetContextInfo(), buttons: bi}
}

// appendSection adds a list's section, leaving out the rows that have no
// ID to answer with. A section with no rows left is left out too.
func appendSection(sections []model.ListSection, title string, rows []model.ListRow) []model.ListSection {
	keep := rows[:0]
	for _, r := range rows {
		if r.ID == "" {
			continue
		}
		r.Title = first(r.Title, r.ID)
		keep = append(keep, r)
	}
	if len(keep) == 0 {
		return sections
	}
	return append(sections, model.ListSection{Title: title, Rows: keep})
}

// nativeButton reads a native flow button's JSON parameters.
func nativeButton(name, params string) (model.Button, bool) {
	var p struct {
		DisplayText string `json:"display_text"`
		ID          string `json:"id"`
		URL         string `json:"url"`
		CopyCode    string `json:"copy_code"`
		// single_select's
		Title    string `json:"title"`
		Sections []struct {
			Title string `json:"title"`
			Rows  []struct {
				ID          string `json:"id"`
				Title       string `json:"title"`
				Description string `json:"description"`
			} `json:"rows"`
		} `json:"sections"`
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
	case "single_select":
		b := model.Button{Kind: model.ButtonList, Label: p.Title}
		for _, sec := range p.Sections {
			var rows []model.ListRow
			for _, r := range sec.Rows {
				rows = append(rows, model.ListRow{ID: r.ID, Title: r.Title, Description: r.Description})
			}
			b.Sections = appendSection(b.Sections, sec.Title, rows)
			b.Label = first(b.Label, sec.Title)
		}
		b.Label = first(b.Label, "Select")
		return b, true
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
func (b *Backend) PressButton(m *model.Message, i int, row string) *model.Message {
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
	// id and label are the answer's ID and what it shows.
	id, label := bt.Value, bt.Label
	switch bt.Kind {
	case model.ButtonReply:
	case model.ButtonList:
		r, ok := listRow(bt.Sections, row)
		if !ok {
			return nil
		}
		id, label = r.ID, r.Title
	default:
		return nil
	}
	sender := b.senderOf(m)
	ci := &waE2E.ContextInfo{
		StanzaID:      proto.String(m.ID),
		Participant:   proto.String(sender.String()),
		QuotedMessage: b.quotedMessage(ctx, m.ChatID, m.ID),
	}
	// The answers to try in turn. The server may refuse button answers
	// (error 479 for native flow answers in groups), so a native flow
	// answer falls back to the older kind, which still carries the ID,
	// and that to a quoted text reply, which gets the words across.
	var msgs []*waE2E.Message
	if bt.Source == srcNative {
		name := "quick_reply"
		if bt.Kind == model.ButtonList {
			name = "single_select"
		}
		params, _ := json.Marshal(map[string]string{"id": id})
		msgs = append(msgs, &waE2E.Message{InteractiveResponseMessage: &waE2E.InteractiveResponseMessage{
			Body: &waE2E.InteractiveResponseMessage_Body{
				Text:   proto.String(label),
				Format: waE2E.InteractiveResponseMessage_Body_DEFAULT.Enum(),
			},
			InteractiveResponseMessage: &waE2E.InteractiveResponseMessage_NativeFlowResponseMessage_{
				NativeFlowResponseMessage: &waE2E.InteractiveResponseMessage_NativeFlowResponseMessage{
					Name:       proto.String(name),
					ParamsJSON: proto.String(string(params)),
					Version:    proto.Int32(3),
				},
			},
			ContextInfo: ci,
		}})
	}
	switch {
	case bt.Kind == model.ButtonList:
		msgs = append(msgs, &waE2E.Message{ListResponseMessage: &waE2E.ListResponseMessage{
			Title:             proto.String(label),
			ListType:          waE2E.ListResponseMessage_SINGLE_SELECT.Enum(),
			SingleSelectReply: &waE2E.ListResponseMessage_SingleSelectReply{SelectedRowID: proto.String(id)},
			ContextInfo:       ci,
		}})
	case bt.Source == srcTemplate || bt.Source == srcNative:
		msgs = append(msgs, &waE2E.Message{TemplateButtonReplyMessage: &waE2E.TemplateButtonReplyMessage{
			SelectedID:          proto.String(id),
			SelectedDisplayText: proto.String(label),
			SelectedIndex:       proto.Uint32(uint32(bt.Index)),
			ContextInfo:         ci,
		}})
	default:
		msgs = append(msgs, &waE2E.Message{ButtonsResponseMessage: &waE2E.ButtonsResponseMessage{
			SelectedButtonID: proto.String(id),
			Response:         &waE2E.ButtonsResponseMessage_SelectedDisplayText{SelectedDisplayText: label},
			Type:             waE2E.ButtonsResponseMessage_DISPLAY_TEXT.Enum(),
			ContextInfo:      ci,
		}})
	}
	msgs = append(msgs, &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text:        proto.String(label),
		ContextInfo: ci,
	}})
	out := &model.Message{
		ID:      cli.GenerateMessageID(),
		ChatID:  m.ChatID,
		FromMe:  true,
		Text:    label,
		Time:    b.sendTime(),
		Receipt: model.Pending,
		Quote:   &model.Quote{ID: m.ID, SenderID: sender.String(), Text: raw.Text, Media: raw.Media},
	}
	return b.storeAndSend(jid, storedMsg{Message: out}, msgs[0], msgs[1:]...)
}

// listRow finds the row of a list whose ID is id.
func listRow(sections []model.ListSection, id string) (model.ListRow, bool) {
	for _, s := range sections {
		for _, r := range s.Rows {
			if r.ID == id {
				return r, true
			}
		}
	}
	return model.ListRow{}, false
}
