package ui

import (
	"gioui.org/font"
	"gioui.org/layout"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// The votes of a poll (WhatsApp's "Poll details") or the answers to an
// event: the Message info panel, listing people under each option.

// openVotes shows who voted in the poll m, or answered the event m.
func (u *UI) openVotes(m *model.Message) {
	u.openMsgInfo(m)
	s := &u.msgInfo
	s.votes, s.data = true, nil
	s.voteList = u.backend.Votes(m)
}

// votesChanged reloads the panel when its poll or event changed: a vote
// came in.
func (u *UI) votesChanged(m *model.Message) {
	s := &u.msgInfo
	if s.open && s.votes && s.chatID == m.ChatID && s.msg.ID == m.ID {
		s.msg = m
		s.voteList = u.backend.Votes(m)
	}
}

func (u *UI) votesTitle() string {
	if u.msgInfo.msg.Event != nil {
		return "Event details"
	}
	return "Poll details"
}

// voteRows heads the panel with the question (or event), then lists each
// option with its count and who picked it, newest first.
func (u *UI) voteRows() []layout.Widget {
	s := &u.msgInfo
	m := s.msg
	rows := []layout.Widget{func(gtx C) D {
		return layout.Inset{Left: infoPadX, Right: 24, Top: 22, Bottom: 6}.Layout(gtx,
			u.label(17, m.Text, u.pal.Text, labelOpts{weight: font.SemiBold, maxLines: 6}).Layout)
	}}
	section := func(title, count string, mine bool, votes []model.Vote) {
		rows = append(rows, u.voteHeading(title, count, mine))
		for _, v := range votes {
			rows = append(rows, u.msgInfoPerson(model.PersonReceipt{ID: v.ID, Name: v.Name}, v.Time))
		}
	}
	if p := m.Poll; p != nil {
		for i, o := range p.Options {
			count := "1 vote"
			if o.Votes != 1 {
				count = itoa(o.Votes) + " votes"
			}
			section(o.Name, count, o.Mine, pollVoters(s.voteList, i))
		}
		return rows
	}
	if m.Event != nil {
		for _, a := range []struct {
			r     model.RSVP
			title string
		}{{model.RSVPGoing, "Going"}, {model.RSVPMaybe, "Maybe"}, {model.RSVPNotGoing, "Not going"}} {
			var votes []model.Vote
			for _, v := range s.voteList {
				if v.RSVP == a.r {
					votes = append(votes, v)
				}
			}
			if len(votes) > 0 {
				section(a.title, itoa(len(votes)), m.Event.Mine == a.r, votes)
			}
		}
	}
	return rows
}

// voteHeading heads the people who picked one option: its name, and how
// many did, with a tick when you are one of them.
func (u *UI) voteHeading(title, count string, mine bool) layout.Widget {
	return func(gtx C) D {
		p := u.pal
		return layout.Inset{Left: infoPadX, Right: 24, Top: 24, Bottom: 10}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, u.label(16, title, p.Text, labelOpts{maxLines: 3}).Layout),
				layout.Rigid(func(gtx C) D {
					if !mine {
						return D{}
					}
					return layout.Inset{Right: 6}.Layout(gtx, iconW(icTick, 18, p.Green))
				}),
				layout.Rigid(u.label(14, count, p.TextSecondary).Layout),
			)
		})
	}
}
