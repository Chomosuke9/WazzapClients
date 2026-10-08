package ui

import (
	"gioui.org/layout"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Custom chat lists as filter chips: WhatsApp adds one chip per list after
// All, Unread, Favourites and Groups, then a "+" that makes a new one.

// chipItem is one chip of the filter row.
type chipItem struct {
	name   string
	filter int    // filterAll... or filterList
	list   string // filterList's list ID
	add    bool   // the "+" chip
}

// chipItems lists the filter row's chips, in order. The buffer is reused
// every frame.
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

// pickChip turns on a chip's filter, or opens New list for the "+".
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

// openNewList opens the New list dialog: a name, then the chats to put in
// it. chats are picked already.
func (u *UI) openNewList(chats []string) {
	u.dialog = dialogState{kind: dialogForward, newList: true, picked: chats}
	u.dialog.search.SingleLine = true
	u.dialog.listName.SingleLine = true
	u.dialog.list.Axis = layout.Vertical
	u.requestFocus(&u.dialog.listName)
}

// createList makes the dialog's list and shows it.
func (u *UI) createList(d *dialogState) {
	name := trimSpace(d.listName.Text())
	if name == "" {
		return
	}
	u.backend.CreateList(name, d.picked)
	u.closeDialog()
}

// listPickable reports whether a chat can go in a list: channels can't.
func listPickable(c *model.Chat) bool { return !isChannelID(c.ID) }
