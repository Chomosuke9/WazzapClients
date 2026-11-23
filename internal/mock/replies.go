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

// seedReplies gives the first announcement of the demo's community replies.
func (b *Backend) seedReplies() {
	ms := b.msgs["work-ann"]
	if len(ms) == 0 {
		return
	}
	m := ms[len(ms)-1]
	if b.replies == nil {
		b.replies = map[string][]model.Reply{}
	}
	b.replies[key(m)] = []model.Reply{
		{ID: "ann-reply-1", SenderID: "clara", Sender: "Clara", Text: "Does that include the *mobile* builds?", Time: m.Time.Add(2 * time.Minute), Receipt: model.Sent},
		{ID: "ann-reply-2", SenderID: "bima", Sender: "Bima", Text: "Got it, thanks for the heads up", Time: m.Time.Add(5 * time.Minute), Receipt: model.Sent},
	}
	m.Replies = 2
}
