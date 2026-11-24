package mock

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Replies implements model.Backend.
func (b *Backend) Replies(m *model.Message) []model.Reply {
	return slices.Clone(b.replies[key(m)])
}

// SendReply implements model.Backend.
func (b *Backend) SendReply(m *model.Message, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	b.replySeq++
	b.setReplies(m, append(b.Replies(m), model.Reply{ID: fmt.Sprintf("reply-%d", b.replySeq), SenderID: "me@lid", Sender: "You",
		Me: true, Text: text, Time: b.now(), Receipt: model.Sent}))
}

// DeleteReply implements model.Backend.
func (b *Backend) DeleteReply(m *model.Message, r model.Reply) {
	b.setReplies(m, slices.DeleteFunc(b.Replies(m), func(x model.Reply) bool { return x.ID == r.ID }))
}

// Reply adds a reply from someone else to m, for tests.
func (b *Backend) Reply(m *model.Message, who, name, text string) {
	b.replySeq++
	b.setReplies(m, append(b.Replies(m), model.Reply{ID: fmt.Sprintf("reply-%d", b.replySeq), SenderID: who, Sender: name,
		Text: text, Time: b.now(), Receipt: model.Sent}))
}

func (b *Backend) setReplies(m *model.Message, rs []model.Reply) {
	if b.replies == nil {
		b.replies = map[string][]model.Reply{}
	}
	b.replies[key(m)] = rs
	b.update(m, func(x *model.Message) { x.Replies = len(rs) })
}

// seedReplies gives the last announcements of the demo's community replies:
// your photo, and the newest.
func (b *Backend) seedReplies() {
	ms := b.msgs["work-ann"]
	if len(ms) < 2 {
		return
	}
	if b.replies == nil {
		b.replies = map[string][]model.Reply{}
	}
	seed := func(m *model.Message, rs ...model.Reply) {
		for i := range rs {
			rs[i].ID = fmt.Sprintf("%s-reply-%d", m.ID, i+1)
			rs[i].Time, rs[i].Receipt = m.Time.Add(time.Duration(2+3*i)*time.Minute), model.Sent
		}
		b.replies[key(m)] = rs
		m.Replies = len(rs)
	}
	seed(ms[len(ms)-2],
		model.Reply{SenderID: "bima", Sender: "Bima", Text: "Is the API work still in Q4?"},
		model.Reply{SenderID: "me@lid", Sender: "You", Me: true, Text: "Yes, right after the freeze"},
		model.Reply{SenderID: "dewi", Sender: "Dewi", Text: "🎉"})
	seed(ms[len(ms)-1],
		model.Reply{SenderID: "clara", Sender: "Clara", Text: "Does that include the *mobile* builds?"},
		model.Reply{SenderID: "bima", Sender: "Bima", Text: "Got it, thanks for the heads up"})
}
