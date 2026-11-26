package ui

import (
	"strings"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Custom chat lists as filter chips: WhatsApp adds one chip per list after
// All, Unread, Favourites and Groups, then ▾, whose menu makes a new one:
// the Create new list page, in the New chat panel.

// chipItem is one chip of the filter row.
type chipItem struct {
	name   string
	filter int    // filterAll... or filterList
	list   string // filterList's list ID
	add    bool   // New list
}

// chipItems lists the filter row's chips, in order, New list last. The
// buffer is reused every frame.
func (u *UI) chipItems() []chipItem {
	out := u.sidebar.chipBuf[:0]
	for i, name := range filterNames {
		out = append(out, chipItem{name: name, filter: i})
	}
	for _, l := range u.lists {
		out = append(out, chipItem{name: l.Name, filter: filterList, list: l.ID})
	}
	out = append(out, chipItem{name: "New list", add: true})
	u.sidebar.chipBuf = out
	return out
}

// activeChip is the index of the chip whose filter is on.
func (u *UI) activeChip(items []chipItem) int {
	for i, it := range items {
		if !it.add && it.filter == u.sidebar.filter && it.list == u.sidebar.listID {
			return i
		}
	}
	return 0
}

// pickChip turns on a chip's filter, or opens Create new list.
func (u *UI) pickChip(it chipItem) {
	if it.add {
		u.openNewList(nil)
		return
	}
	u.sidebar.filter, u.sidebar.listID = it.filter, it.list
	u.sidebar.list.Position = layout.Position{}
}

// updateChips reads the chips' clicks.
func (u *UI) updateChips(gtx C) {
	for i, it := range u.chipItems() {
		if u.btn("chip:" + itoa(i)).Clicked(gtx) {
			u.pickChip(it)
		}
	}
	if u.btn("chip:more").Clicked(gtx) {
		u.openChipMenu()
	}
}

// loadLists reads the custom lists again, once per frame however many
// events asked for it.
func (u *UI) loadLists() {
	u.listsStale = false
	u.lists = u.backend.Lists()
	u.sidebar.listSetFor = ""
	if u.sidebar.filter != filterList {
		return
	}
	for _, l := range u.lists {
		if l.ID == u.sidebar.listID {
			return
		}
	}
	// The open list was deleted.
	u.sidebar.filter, u.sidebar.listID = filterAll, ""
}

// inOpenList reports whether a chat is in the list the chips filter by.
func (u *UI) inOpenList(id string) bool {
	s := &u.sidebar
	if s.listSetFor != s.listID || s.listSet == nil {
		clear(s.listSet)
		if s.listSet == nil {
			s.listSet = make(map[string]bool)
		}
		for _, l := range u.lists {
			if l.ID == s.listID {
				for _, c := range l.Chats {
					s.listSet[c] = true
				}
			}
		}
		s.listSetFor = s.listID
	}
	return s.listSet[id]
}

// openNewList slides the Create new list page in over the chat list, with
// chats picked already.
func (u *UI) openNewList(chats []string) {
	nc := &u.newChat
	u.newChatPage()
	nc.start(nil)
	nc.step, nc.base = ncList, ncList
	nc.listPicked = chats
	nc.listName.SetText("")
	nc.listSearch.SetText("")
	nc.llist.Position, nc.lclist.Position = layout.Position{}, layout.Position{}
	u.requestFocus(&nc.listName)
}

// createList makes the page's list.
func (u *UI) createList() {
	nc := &u.newChat
	name := trimSpace(nc.listName.Text())
	if name == "" || nc.step != ncList {
		return
	}
	u.backend.CreateList(name, nc.listPicked)
	nc.step = ncNone
}

// updateNewList reads the Create new list pages' clicks.
func (u *UI) updateNewList(gtx C) {
	nc := &u.newChat
	for {
		ev, ok := nc.listName.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			u.createList()
		}
	}
	if u.btn("nc:createlist").Clicked(gtx) {
		u.createList()
	}
	if u.btn("nc:listadd").Clicked(gtx) && nc.step == ncList {
		nc.step = ncListChats
		nc.listSearch.SetText("")
		nc.lclist.Position = layout.Position{}
		u.requestFocus(&nc.listSearch)
	}
	if u.btn("nc:listdone").Clicked(gtx) && nc.step == ncListChats {
		u.newChatBack()
	}
	if nc.step < ncList {
		return
	}
	for _, c := range u.chats {
		if u.btn("nc:l:" + c.ID).Clicked(gtx) {
			if i := indexOf(nc.listPicked, c.ID); i >= 0 {
				nc.listPicked = append(nc.listPicked[:i:i], nc.listPicked[i+1:]...)
			} else {
				nc.listPicked = append(nc.listPicked, c.ID)
			}
		}
		if i := indexOf(nc.listPicked, c.ID); i >= 0 && u.btn("nc:lx:"+c.ID).Clicked(gtx) {
			nc.listPicked = append(nc.listPicked[:i:i], nc.listPicked[i+1:]...)
		}
	}
}

// layoutNewListPage is Create new list: its name, then the chats in it.
func (u *UI) layoutNewListPage(gtx C) D {
	nc := &u.newChat
	rows := []layout.Widget{
		func(gtx C) D {
			return u.sectionLabel(gtx, "List name", layout.Inset{Left: 24, Right: 24, Top: 6}, labelOpts{})
		},
		func(gtx C) D { return u.pollField(gtx, &nc.listName, "List name") },
		func(gtx C) D {
			return u.sectionLabel(gtx, "Included", layout.Inset{Left: 24, Right: 24, Top: 40, Bottom: 12}, labelOpts{})
		},
		func(gtx C) D {
			return u.ncRow(gtx, u.btn("nc:listadd"), u.greenCircle(icAdd), "Add people or groups", "")
		},
	}
	for _, id := range nc.listPicked {
		if c := u.chatByID(id); c != nil {
			rows = append(rows, func(gtx C) D { return u.newListRow(gtx, c) })
		}
	}
	rows = append(rows, func(gtx C) D {
		return layout.Inset{Left: 24, Top: 24, Bottom: 24}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(132))
			return u.wideButton(gtx, "nc:createlist", "Create list", trimSpace(nc.listName.Text()) == "")
		})
	})
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.ncHeader(gtx, "Create new list") }),
		layout.Flexed(1, func(gtx C) D {
			return u.scrollList(gtx, &nc.llist, len(rows), func(gtx C, i int) D { return rows[i](gtx) })
		}),
	)
}

// newListRow is a chat in the new list, with a cross to take it out.
func (u *UI) newListRow(gtx C, c *model.Chat) D {
	p := u.pal
	return vcenter(gtx, gtx.Dp(72), func(gtx C) D {
		return layout.Inset{Left: 24, Right: 21}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(u.listChatPic(c)),
				layout.Rigid(layout.Spacer{Width: 15}.Layout),
				layout.Flexed(1, u.label(17, u.listName(c), p.Text, labelOpts{maxLines: 1}).Layout),
				layout.Rigid(func(gtx C) D { return u.iconButton(gtx, u.btn("nc:lx:"+c.ID), icClose, 40, 22, p.Icon) }),
			)
		})
	})
}

// listChatPic is a chat's picture, as the chat list shows it.
func (u *UI) listChatPic(c *model.Chat) layout.Widget {
	if cm := u.inCommunity[c.ID]; cm != nil && cm.Announcements == c.ID {
		return func(gtx C) D { return u.avatarOf(gtx, cm.ID, avatarCommunity, 49) }
	}
	return func(gtx C) D { return u.avatar(gtx, c.ID, c.Name, c.IsGroup, 49) }
}

// layoutListChatsPage is Add people or groups: the chats, those in the new
// list ticked. Lists hold chats, so it leaves out your other contacts.
func (u *UI) layoutListChatsPage(gtx C) D {
	nc := &u.newChat
	q := strings.ToLower(trimSpace(nc.listSearch.Text()))
	rows := nc.listRows[:0] // reused every frame
	for _, c := range u.chats {
		if listPickable(c) && (q == "" || strings.Contains(strings.ToLower(u.listName(c)), q)) {
			rows = append(rows, c)
		}
	}
	nc.listRows = rows
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.ncHeader(gtx, "Add people or groups") }),
		layout.Rigid(func(gtx C) D { return u.ncSearch(gtx, &nc.listSearch, "Search name") }),
		layout.Flexed(1, func(gtx C) D {
			if len(rows) == 0 {
				return u.ncNote(gtx, "No results found for '"+trimSpace(nc.listSearch.Text())+"'")
			}
			return u.scrollList(gtx, &nc.lclist, len(rows), func(gtx C, i int) D {
				c := rows[i]
				box, col := icCheckBoxEmpty, u.pal.TextSecondary
				if indexOf(nc.listPicked, c.ID) >= 0 {
					box, col = icCheckBox, u.pal.Green
				}
				return u.ncRow(gtx, u.btn("nc:l:"+c.ID), func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(iconW(box, 24, col)),
						layout.Rigid(layout.Spacer{Width: 16}.Layout),
						layout.Rigid(u.listChatPic(c)),
					)
				}, u.listName(c), "")
			})
		}),
		layout.Rigid(func(gtx C) D {
			return u.ncButton(gtx, "nc:listdone", &nc.listIn, icTick, len(nc.listPicked) > 0, false, u.pal.Panel)
		}),
	)
}

// listPickable reports whether a chat can go in a list: channels can't.
func listPickable(c *model.Chat) bool { return !isChannelID(c.ID) && !c.Locked }
