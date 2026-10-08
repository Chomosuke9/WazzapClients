package mock

import (
	"slices"
	"strings"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// reacts counts emojis, one per person, for a demo message's Reactions:
// the most given first, ties in the order given.
func reacts(emojis ...string) []model.ReactionCount {
	var out []model.ReactionCount
	for _, e := range emojis {
		i := slices.IndexFunc(out, func(c model.ReactionCount) bool { return c.Emoji == e })
		if i < 0 {
			out = append(out, model.ReactionCount{Emoji: e})
			i = len(out) - 1
		}
		out[i].Count++
	}
	slices.SortStableFunc(out, func(a, b model.ReactionCount) int { return b.Count - a.Count })
	return out
}

// seedReactors gives the reactions the demo data counts to people: you
// for MyReaction, then the chat's person or the group's senders other than
// the message's.
func (b *Backend) seedReactors() {
	b.reactors = map[string][]model.Reactor{}
	for _, c := range b.chats {
		var people []model.Reactor
		if c.IsGroup {
			for _, m := range b.msgs[c.ID] {
				if m.SenderID != "" && !slices.ContainsFunc(people, func(r model.Reactor) bool { return r.ID == m.SenderID }) {
					people = append(people, model.Reactor{ID: m.SenderID, Name: m.Sender})
				}
			}
		} else {
			people = []model.Reactor{{ID: c.ID, Name: c.Name}}
		}
		for _, m := range b.msgs[c.ID] {
			if len(m.Reactions) == 0 {
				continue
			}
			others := slices.DeleteFunc(slices.Clone(people), func(r model.Reactor) bool { return r.ID == m.SenderID })
			var rs []model.Reactor
			mineLeft := m.MyReaction != ""
			n := 0
			for _, rc := range m.Reactions {
				for range rc.Count {
					at := m.Time.Add(time.Duration(len(rs)+1) * time.Minute)
					if mineLeft && rc.Emoji == m.MyReaction {
						mineLeft = false
						rs = append(rs, model.Reactor{ID: "me@lid", Name: "You", Me: true, Emoji: rc.Emoji, Time: at})
						continue
					}
					p := model.Reactor{ID: "someone", Name: "Someone"}
					if len(others) > 0 {
						p = others[n%len(others)]
						n++
					}
					p.Emoji, p.Time = rc.Emoji, at
					rs = append(rs, p)
				}
			}
			b.reactors[key(m)] = rs
		}
	}
}

// demoReactions reacts to Clara's release candidate in the Product Team
// with several emojis, yours among them.
func (b *Backend) demoReactions() {
	for _, m := range b.msgs["work"] {
		if strings.HasPrefix(m.Text, "Release candidate") {
			m.Reactions, m.MyReaction = reacts("👍", "❤️", "👍", "🎉"), "👍"
		}
	}
}

// recountReactions counts a message's reactions from its reactors.
func (b *Backend) recountReactions(m *model.Message) {
	rs := slices.Clone(b.reactors[key(m)])
	slices.SortStableFunc(rs, func(a, b model.Reactor) int { return a.Time.Compare(b.Time) })
	var emojis []string
	m.MyReaction = ""
	for _, r := range rs {
		emojis = append(emojis, r.Emoji)
		if r.Me {
			m.MyReaction = r.Emoji
		}
	}
	m.Reactions = reacts(emojis...)
}

func (b *Backend) React(m *model.Message, emoji string) {
	x := b.find(m)
	if x == nil {
		return
	}
	if b.reactors == nil {
		b.reactors = map[string][]model.Reactor{}
	}
	k := key(x)
	rs := slices.DeleteFunc(b.reactors[k], func(r model.Reactor) bool { return r.Me })
	if emoji != "" {
		rs = append(rs, model.Reactor{ID: "me@lid", Name: "You", Me: true, Emoji: emoji, Time: b.now()})
	}
	b.reactors[k] = rs
	b.update(x, b.recountReactions)
}

// Reactors lists you first, then the newest first.
func (b *Backend) Reactors(m *model.Message) []model.Reactor {
	rs := slices.Clone(b.reactors[key(m)])
	slices.SortStableFunc(rs, func(a, b model.Reactor) int {
		if a.Me != b.Me {
			if a.Me {
				return -1
			}
			return 1
		}
		return b.Time.Compare(a.Time)
	})
	return rs
}
