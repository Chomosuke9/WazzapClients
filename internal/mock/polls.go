package mock

import (
	"slices"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// demoVotes votes in the demo's poll and answers its event.
func (b *Backend) demoVotes() {
	b.votes = map[string][]model.Vote{}
	now := b.now()
	ago := func(m int) time.Time { return now.Add(-time.Duration(m) * time.Minute) }
	for _, m := range b.msgs["design"] {
		if m.Poll != nil {
			b.votes[key(m)] = []model.Vote{
				{ID: "clara", Name: "Clara", Time: ago(20), Options: []int{0}},
				{ID: "dewi", Name: "Dewi", Time: ago(25), Options: []int{0}},
				{ID: "andre", Name: "Andre", Time: ago(40), Options: []int{2}},
			}
			b.recount(m)
		}
	}
	for _, m := range b.msgs["family"] {
		if m.Event != nil {
			b.votes[key(m)] = []model.Vote{
				{ID: "dad", Name: "Dad", Time: ago(10), RSVP: model.RSVPGoing},
				{ID: "dimas", Name: "Dimas", Time: ago(12), RSVP: model.RSVPGoing},
				{ID: "sari", Name: "Sari", Time: ago(15), RSVP: model.RSVPMaybe},
				{ID: "mom", Name: "Mom", Time: ago(30), RSVP: model.RSVPGoing},
			}
			b.recount(m)
		}
	}
}

func key(m *model.Message) string { return m.ChatID + "/" + m.ID }

// recount counts a poll's or event's votes.
func (b *Backend) recount(m *model.Message) {
	if p := m.Poll; p != nil {
		p.Voters = 0
		for i := range p.Options {
			p.Options[i].Votes, p.Options[i].Mine, p.Options[i].Faces = 0, false, nil
		}
		for _, v := range b.votes[key(m)] {
			p.Voters++
			for _, i := range v.Options {
				o := &p.Options[i]
				o.Votes++
				o.Mine = o.Mine || v.Me
				if len(o.Faces) < 3 {
					o.Faces = append(o.Faces, v.ID)
				}
			}
		}
	}
	if e := m.Event; e != nil {
		e.Going, e.Maybe, e.NotGoing, e.Mine = 0, 0, 0, model.RSVPNone
		for _, v := range b.votes[key(m)] {
			switch v.RSVP {
			case model.RSVPGoing:
				e.Going++
			case model.RSVPMaybe:
				e.Maybe++
			case model.RSVPNotGoing:
				e.NotGoing++
			}
			if v.Me {
				e.Mine = v.RSVP
			}
		}
	}
}

// deepCopy copies a message deep enough that the UI's copy doesn't change
// with the demo's.
func deepCopy(m *model.Message) *model.Message {
	cp := *m
	if m.Poll != nil {
		p := *m.Poll
		p.Options = slices.Clone(p.Options)
		cp.Poll = &p
	}
	if m.Event != nil {
		e := *m.Event
		cp.Event = &e
	}
	return &cp
}

func (b *Backend) VotePoll(m *model.Message, options []int) {
	x := b.find(m)
	if x == nil || x.Poll == nil {
		return
	}
	if b.votes == nil {
		b.votes = map[string][]model.Vote{}
	}
	k := key(x)
	vs := slices.DeleteFunc(b.votes[k], func(v model.Vote) bool { return v.Me })
	if len(options) > 0 {
		vs = append([]model.Vote{{ID: "me@lid", Name: "You", Me: true, Time: b.now(), Options: slices.Clone(options)}}, vs...)
	}
	b.votes[k] = vs
	b.update(x, b.recount)
}

func (b *Backend) Votes(m *model.Message) []model.Vote {
	return slices.Clone(b.votes[key(m)])
}
