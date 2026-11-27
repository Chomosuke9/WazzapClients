package wa

import (
	"bytes"
	"slices"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// What a reply of ours quotes is what other clients show of the quoted
// message: the caption shown, the people it names, and nothing it shows
// only once.
func TestQuotedMessageSent(t *testing.T) {
	b := testBackend(t)
	const chat = "123@g.us"

	// A photo replying to something and mentioning someone, its caption edited.
	img := &waE2E.ImageMessage{URL: proto.String("https://mmg/x"), DirectPath: proto.String("/v/x"),
		MediaKey: bytes.Repeat([]byte{1}, 32), JPEGThumbnail: []byte("photo"), Caption: proto.String("first"),
		ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("older"), QuotedMessage: &waE2E.Message{Conversation: proto.String("older")},
			IsForwarded: proto.Bool(true)}}
	e := snippetEvent("photo", &waE2E.Message{ImageMessage: img})
	b.onMessage(e)
	ed := snippetEvent("photo", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("look @789"),
		ContextInfo: &waE2E.ContextInfo{MentionedJID: []string{"789@lid"}}}})
	ed.IsEdit = true
	ed.Info.Timestamp = e.Info.Timestamp.Add(time.Minute)
	b.onMessage(ed)
	q := b.quotedMessage(b.ctx, chat, "photo").GetImageMessage()
	if q.GetCaption() != "look @789" || !bytes.Equal(q.GetJPEGThumbnail(), []byte("photo")) {
		t.Errorf("photo quoted as %q, thumbnail %q", q.GetCaption(), q.GetJPEGThumbnail())
	}
	if ci := q.GetContextInfo(); ci.GetQuotedMessage() != nil || ci.GetStanzaID() != "" || ci.GetIsForwarded() ||
		!slices.Equal(ci.GetMentionedJID(), []string{"789@lid"}) {
		t.Errorf("photo's quoted context: %v", ci)
	}

	// A view once photo, and view once text, go as what they are alone.
	b.onMessage(snippetEvent("once", &waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{URL: proto.String("https://mmg/y"), DirectPath: proto.String("/v/y"),
			MediaKey: bytes.Repeat([]byte{2}, 32), JPEGThumbnail: []byte("secret"), Caption: proto.String("secret caption"),
			ViewOnce: proto.Bool(true)}}}}))
	once := b.quotedMessage(b.ctx, chat, "once")
	im := once.GetViewOnceMessageV2().GetMessage().GetImageMessage()
	if im == nil || !im.GetViewOnce() || len(im.GetJPEGThumbnail()) > 0 || im.GetMediaKey() != nil || im.GetCaption() != "" {
		t.Errorf("view once photo quoted as %v", once)
	}
	b.onMessage(snippetEvent("oncetext", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String("secret text"), ViewOnce: proto.Bool(true)}}))
	if q := b.quotedMessage(b.ctx, chat, "oncetext"); q.GetViewOnceMessageV2().GetMessage().GetExtendedTextMessage().GetText() != "" ||
		!isViewOnce(q) {
		t.Errorf("view once text quoted as %v", q)
	}

	// Replies to them show what they are here, not what they hold.
	for i, id := range []string{"once", "oncetext"} {
		ci := &waE2E.ContextInfo{StanzaID: proto.String(id), Participant: proto.String("456@lid"),
			QuotedMessage: b.quotedMessage(b.ctx, chat, id)}
		rid := "re" + id
		b.onMessage(snippetEvent(rid, &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String("ok"), ContextInfo: ci}}))
		// A client that quotes them in full shows nothing of them here either.
		full := snippetEvent(rid+"full", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String("ok"), ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String(id),
				Participant: proto.String("456@lid"), QuotedMessage: []*waE2E.Message{
					{ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
						Caption: proto.String("secret caption"), ViewOnce: proto.Bool(true)}}}},
					{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("secret text"), ViewOnce: proto.Bool(true)}},
				}[i]}}})
		b.onMessage(full)
	}
	for _, m := range b.Messages(chat, 50) {
		if m.Quote == nil || (m.Quote.ID != "once" && m.Quote.ID != "oncetext") {
			continue
		}
		if !m.Quote.ViewOnce || m.Quote.Text != "" {
			t.Errorf("%s quotes %+v", m.ID, m.Quote)
		}
	}

	// Your own reply's quote, before it's read back.
	own := b.quote(chat, &model.Message{ID: "once", ChatID: chat, SenderID: "456@lid", Kind: model.KindViewOnce,
		Media: model.MediaImage, Text: "secret caption"}, &waE2E.ContextInfo{})
	if !own.ViewOnce || own.Text != "" {
		t.Errorf("own reply's quote %+v", own)
	}
}
