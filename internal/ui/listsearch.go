package ui

import (
	"strings"

	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// The chat list's search. Like WhatsApp's, a query lists the chats whose
// name matches it (archived ones too), then your contacts you have no
// chat with, then the messages of every chat that contain it.

// listSearchState is the chat list search's contacts and messages.
type listSearchState struct {
	ran, got string           // the query sent to the backend, and the one msgs are for
	msgs     []*model.Message // found messages, newest first
	contacts []*model.Contact // read when a search starts
	rows     []findRow        // the list's rows, a buffer reused every frame
}

// listSearchLimit is how many messages the chat list's search lists.
const listSearchLimit = 60

// findRow is one row of the results: a section's heading, a chat, a
// contact or a message.
type findRow struct {
	heading string
	chat    *model.Chat
	contact *model.Contact
	msg     *model.Message
}

// listQuery is the chat list's search query, trimmed.
func (u *UI) listQuery() string { return trimSpace(u.sidebar.search.Text()) }

// runListSearch starts a message search when the query changed.
func (u *UI) runListSearch(q string) {
	f := &u.sidebar.find
	if q == f.ran {
		return
	}
	if f.ran == "" {
		f.contacts = u.backend.Contacts()
	}
	f.ran = q
	u.sidebar.list.Position = layout.Position{}
	if q == "" {
		f.got, f.msgs, f.contacts = "", nil, nil
		return
	}
	u.backend.SearchMessages("", q, listSearchLimit)
}

// listSearchResults takes the chat list search's messages.
func (u *UI) listSearchResults(e model.SearchEvent) {
	f := &u.sidebar.find
	if e.Query == f.ran {
		f.got, f.msgs = e.Query, e.Msgs
	}
}

// findRows lays out the results: the chats found (u.sidebar.visible),
// then, under the All filter, contacts and messages.
func (u *UI) findRows(q string) []findRow {
	f := &u.sidebar.find
	rows := f.rows[:0]
	if len(u.sidebar.visible) > 0 {
		rows = append(rows, findRow{heading: "Chats"})
		for _, c := range u.sidebar.visible {
			rows = append(rows, findRow{chat: c})
		}
	}
	if u.sidebar.filter == filterAll && !u.sidebar.showArchived {
		lq := strings.ToLower(q)
		digits := phoneDigits(q)
		start := len(rows)
		for _, c := range f.contacts {
			if u.chatByID(c.ID) != nil {
				continue // listed under Chats if it matches
			}
			if !strings.Contains(strings.ToLower(c.Name), lq) &&
				(digits == "" || !strings.Contains(phoneDigits(c.Phone), digits)) {
				continue
			}
			if len(rows) == start {
				rows = append(rows, findRow{heading: "Contacts"})
			}
			rows = append(rows, findRow{contact: c})
		}
		if f.got == q && len(f.msgs) > 0 {
			rows = append(rows, findRow{heading: "Messages"})
			for _, m := range f.msgs {
				rows = append(rows, findRow{msg: m})
			}
		}
	}
	f.rows = rows
	return rows
}

// layoutFindList draws the search's results in place of the chat list.
func (u *UI) layoutFindList(gtx C, q string) D {
	p := u.pal
	rows := u.findRows(q)
	if len(rows) == 0 {
		if u.sidebar.find.got != q && u.sidebar.filter == filterAll && !u.sidebar.showArchived {
			return D{Size: gtx.Constraints.Max} // still looking through messages
		}
		return layout.Inset{Left: 40, Right: 40, Top: 48}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return u.label(15, "No chats, contacts or messages found", p.TextSecondary, labelOpts{align: text.Middle}).Layout(gtx)
		})
	}
	return u.scrollList(gtx, &u.sidebar.list, len(rows), func(gtx C, i int) D {
		r := rows[i]
		switch {
		case r.heading != "":
			top := unit.Dp(20)
			if i == 0 {
				top = 6
			}
			return u.sectionLabel(gtx, r.heading, layout.Inset{Left: 34, Right: 34, Top: top, Bottom: 10}, labelOpts{})
		case r.chat != nil:
			return u.layoutChatRow(gtx, r.chat)
		case r.contact != nil:
			c := r.contact
			btn := u.btn("findcontact:" + c.ID)
			if btn.Clicked(gtx) {
				u.openDirect(c.ID, c.Name)
			}
			return u.ncRow(gtx, btn, u.contactPic(c), c.Name, c.Phone)
		default:
			return u.findMessage(gtx, r.msg, q)
		}
	})
}

// findMessage is a message the search found: its chat's name and date,
// then its text with the matches in green.
func (u *UI) findMessage(gtx C, m *model.Message, q string) D {
	p := u.pal
	btn := u.btn("findmsg:" + m.ChatID + "/" + m.ID)
	if btn.Clicked(gtx) {
		u.showMessage(m)
	}
	c := u.chatByID(m.ChatID)
	name := ""
	if c != nil {
		name = u.listName(c)
	}
	return layout.Inset{Left: 13, Right: 18, Top: 2, Bottom: 2}.Layout(gtx, func(gtx C) D {
		return clickable(gtx, btn, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			bg := mix(p.Panel, p.Hover, u.hover(gtx, btn))
			return background(gtx, u.rowBg(bg), 10, func(gtx C) D {
				return layout.Inset{Left: 21, Right: 14, Top: 11, Bottom: 12}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
								layout.Flexed(1, u.label(17, name, p.Text, labelOpts{maxLines: 1}).Layout),
								layout.Rigid(layout.Spacer{Width: 6}.Layout),
								layout.Rigid(u.label(13.2, listTime(m.Time, u.now()), p.TextSecondary, labelOpts{maxLines: 1}).Layout),
							)
						}),
						layout.Rigid(layout.Spacer{Height: 3}.Layout),
						layout.Rigid(func(gtx C) D { return u.searchResultText(gtx, c, m, q, 1) }),
					)
				})
			})
		})
	})
}
