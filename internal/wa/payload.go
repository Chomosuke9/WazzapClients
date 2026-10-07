package wa

import (
	"context"
	"strings"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// A stored message is its payload: the waE2E protobuf it came or went as
// (raw_payload, which /catch shows), and its latest edit's
// (edit_payload). Everything it shows is read from them here, as parse
// reads a message that arrives, so there is no format of the app's own
// to keep in step with them. The columns beside them are the envelope
// (sender, time), what happened to the message since (receipts,
// reactions, stars...), and kind, media and text, kept for queries.

// fill completes a message loaded from its columns with what its payloads
// hold. A deleted message has none, and an unsupported one shows nothing
// of it.
func (r *rawMsg) fill(raw, edit []byte) {
	m := r.Message
	if m.Kind == model.KindDeleted || m.Kind == model.KindUnsupported {
		return
	}
	c, pm := describeRaw(raw)
	if pm == nil {
		return
	}
	m.Thumb, r.hasBlob = c.keptThumb(), c.keepsBlob(pm)
	m.Duration = c.duration
	m.Link = c.link
	c.file.apply(m)
	if m.Kind == model.KindImage {
		m.Album = albumOf(pm)
	}
	m.Forwarded = c.ctx.GetIsForwarded()
	if !c.buttons.empty() {
		r.buttons = c.buttons
		c.buttons.apply(m)
	}
	r.quote = quoteOf(c.ctx)
	// The text shown is the edit's (the text column), and so are its
	// mentions; an event's edit changes the event.
	ctx := c.ctx
	if ec, em := describeRaw(edit); em != nil {
		ctx = ec.ctx
		if ec.extra.Event != nil {
			c.extra.Event = ec.extra.Event
		}
	}
	c.extra.apply(m)
	r.mentions = strings.Join(mentionsOf(ctx), ",")
}

// describeRaw describes a stored payload, and returns it unmarshaled, or
// nil when there is none.
func describeRaw(raw []byte) (content, *waE2E.Message) {
	var m waE2E.Message
	if len(raw) == 0 || proto.Unmarshal(raw, &m) != nil {
		return content{}, nil
	}
	return describe(&m), &m
}

// rawMedia returns the thumbnail and media blob a stored payload holds,
// or nils.
func rawMedia(raw []byte) (thumb, blob []byte) {
	c, m := describeRaw(raw)
	if m == nil {
		return nil, nil
	}
	return c.keptThumb(), c.keptBlob(m)
}

// rawText returns the text and mentions of a stored payload.
func rawText(raw []byte) (text, mentions string) {
	c, _ := describeRaw(raw)
	return c.text, strings.Join(mentionsOf(c.ctx), ",")
}

// payloadView returns m as it reads back once stored with the payload
// msg: what a file kept on disk for it is named after.
func payloadView(m *model.Message, msg *waE2E.Message) *model.Message {
	cp := *m
	r := rawMsg{Message: &cp}
	r.fill(marshal(msg), nil)
	return r.Message
}

// rawQuote is what a payload says about the message it replies to.
type rawQuote struct {
	id     string
	sender string // as the payload names them
	text   string
	media  model.Media
	quoted bool // the payload carries the quoted message
}

// quoteOf reads the quote in a message's context, or returns nil.
func quoteOf(ci *waE2E.ContextInfo) *rawQuote {
	id, q := ci.GetStanzaID(), ci.GetQuotedMessage()
	if id == "" && q == nil {
		return nil
	}
	qc := describe(q)
	return &rawQuote{id: id, sender: ci.GetParticipant(), text: qc.text, media: qc.media, quoted: q != nil}
}

// resolveQuote turns a payload's quote in chat into the one shown. The
// quoted message normally travels with the reply; when it is missing (or
// of a type that isn't rendered) the original is looked up by its ID.
func (b *Backend) resolveQuote(ctx context.Context, chat string, q *rawQuote) *model.Quote {
	if q == nil {
		return nil
	}
	quote := &model.Quote{ID: q.id, Text: q.text, Media: q.media}
	sender := q.sender
	if q.text == "" && q.media == model.MediaNone && q.id != "" {
		orig, ok := b.store.message(ctx, chat, q.id)
		switch {
		case ok:
			quote.Text, quote.Media = orig.Text, orig.Media
			switch orig.Kind {
			case model.KindDeleted:
				quote.Text = "This message was deleted"
			case model.KindUnsupported:
				quote.Text = "This message couldn't load"
			}
			if sender == "" {
				sender = orig.senderJID
				if orig.FromMe {
					sender = b.ownJID(chat).String()
				}
			}
		case !q.quoted:
			return nil // only an ID, of a message we don't have
		}
	}
	if j, err := types.ParseJID(sender); err == nil && !j.IsEmpty() {
		sender = b.canonical(ctx, j).String()
	}
	quote.SenderID = sender
	return quote
}
