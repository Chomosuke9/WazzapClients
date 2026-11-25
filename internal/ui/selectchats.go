package ui

import (
	"image"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// Select chats (the chat list's ⋮ menu): checkboxes on the rows, and a bar
// in place of the search and the chips whose ⋮ marks the chats picked read
// or unread, mutes, archives, pins, clears or deletes them, like WhatsApp's.
// Clicking a row still opens its chat: only its checkbox picks it.

// chatPick is the chat list's select mode.
type chatPick struct {
	on     bool
	picked map[string]bool
	anim   tween // the checkboxes sliding in
}

func (u *UI) startChatSelect() {
	u.sidebar.search.SetText("") // the bar takes its place
	u.sidebar.pick = chatPick{on: true, picked: map[string]bool{}, anim: u.sidebar.pick.anim}
}

func (u *UI) endChatSelect() {
	u.sidebar.pick.on = false
	u.sidebar.pick.picked = nil
}

// pickedChats returns the chats picked, in the list's order.
func (u *UI) pickedChats() []*model.Chat {
	var out []*model.Chat
	for _, c := range u.chats {
		if u.sidebar.pick.picked[c.ID] {
			out = append(out, c)
		}
	}
	return out
}

// pickRow toggles a chat in select mode.
func (u *UI) pickRow(id string) {
	s := &u.sidebar.pick
	if s.picked[id] {
		delete(s.picked, id)
	} else {
		s.picked[id] = true
	}
}

// pickActions runs the select bar's buttons and the rows' checkboxes.
func (u *UI) pickActions(gtx C) {
	if !u.sidebar.pick.on {
		return
	}
	if u.btn("pick:cancel").Clicked(gtx) {
		u.endChatSelect()
		return
	}
	if u.btn("pick:menu").Clicked(gtx) {
		u.openPickMenu()
	}
	for _, c := range u.sidebar.visible {
		if u.btn("pickbox:" + c.ID).Clicked(gtx) {
			u.pickRow(c.ID)
		}
	}
}

// openPickMenu opens the select bar's ⋮ menu at the pointer.
func (u *UI) openPickMenu() { u.ctx = ctxMenu{kind: ctxPick, at: u.mouse} }

// pickItems are the select bar's actions on the chats picked, nil without
// any. Pin and Delete are ours.
func (u *UI) pickItems() []menuItem {
	chats := u.pickedChats()
	if len(chats) == 0 {
		return nil
	}
	item := func(key string, ic *icon.Icon, label string) menuItem {
		return menuItem{key: "pick-" + key, ic: ic, label: label, run: func() { u.pickAction(key) }}
	}
	read, mute := item("read", icMarkUnread, "Mark as unread"), item("mute", icMuted, "Mute notifications")
	archive, pin := item("archive", icArchive, "Archive chat"), item("pin", icPin, "Pin chat")
	if u.pickedUnread(chats) {
		read.label = "Mark as read"
	}
	if allChats(chats, func(c *model.Chat) bool { return c.Muted }) {
		mute.ic, mute.label = icBellLine, "Unmute notifications"
	}
	if allChats(chats, func(c *model.Chat) bool { return c.Archived }) {
		archive.label = "Unarchive chat"
	}
	if allChats(chats, func(c *model.Chat) bool { return c.Pinned }) {
		pin.label = "Unpin chat"
	}
	return []menuItem{read, mute, archive, pin, {divider: true},
		item("clear", icClear, "Clear selected chats"), item("delete", icDelete, "Delete selected chats")}
}

// allChats reports whether f is true of every chat.
func allChats(chats []*model.Chat, f func(c *model.Chat) bool) bool {
	for _, c := range chats {
		if !f(c) {
			return false
		}
	}
	return true
}

// pickAction runs a select bar action on the chats picked. Each one but
// a mute's durations and what asks first ends select mode, as in WhatsApp.
func (u *UI) pickAction(a string) {
	chats := u.pickedChats()
	if len(chats) == 0 {
		return
	}
	b := u.backend
	all := func(f func(c *model.Chat) bool) bool { return allChats(chats, f) }
	switch a {
	case "archive":
		archive := !all(func(c *model.Chat) bool { return c.Archived })
		for _, c := range chats {
			if c.Archived != archive {
				b.SetArchived(c.ID, archive)
			}
		}
	case "mute":
		if !all(func(c *model.Chat) bool { return c.Muted }) {
			u.ctx = ctxMenu{kind: ctxPickMute, at: u.mouse}
			return
		}
		for _, c := range chats {
			b.SetMuted(c.ID, false, 0)
		}
	case "pin":
		pin := !all(func(c *model.Chat) bool { return c.Pinned })
		for _, c := range chats {
			if c.Pinned != pin && !c.Archived {
				b.SetPinned(c.ID, pin)
			}
		}
	case "read":
		if u.pickedUnread(chats) {
			var ids []string
			for _, c := range chats {
				if c.Unread != 0 {
					ids = append(ids, c.ID)
				}
			}
			b.MarkRead(ids)
		} else {
			for _, c := range chats {
				b.SetUnread(c.ID, true)
				if u.selected != nil && u.selected.ID == c.ID {
					u.closeChat()
				}
			}
		}
	case "clear":
		u.confirmClearChats(chats)
		return
	case "delete":
		u.confirmDeleteChats(chats)
		return
	}
	u.endChatSelect()
}

// pickedUnread reports whether any chat picked is unread, so the bar's
// button marks them read rather than unread.
func (u *UI) pickedUnread(chats []*model.Chat) bool {
	for _, c := range chats {
		if c.Unread != 0 {
			return true
		}
	}
	return false
}

// confirmClearChats clears the chats picked.
func (u *UI) confirmClearChats(chats []*model.Chat) {
	title, label := "Clear this chat?", "Clear chat"
	if len(chats) > 1 {
		title, label = "Clear "+itoa(len(chats))+" chats?", "Clear chats"
	}
	u.confirm(title, "Messages will only be removed from this device and your devices on the newer versions of WhatsApp.",
		dialogButton{label: label, primary: true, run: func() {
			for _, c := range chats {
				u.backend.ClearChat(c.ID)
			}
			u.endChatSelect()
		}})
}

// confirmDeleteChats deletes the chats picked. Groups you're still in are
// left: WhatsApp has you exit a group before deleting it.
func (u *UI) confirmDeleteChats(chats []*model.Chat) {
	var ids []string
	groups := 0
	for _, c := range chats {
		if c.IsGroup && !c.Left {
			groups++
		} else {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) == 0 {
		u.inform("Exit the groups first", "To delete a group, exit it from its menu first.")
		return
	}
	title := "Delete this chat?"
	if len(ids) > 1 {
		title = "Delete " + itoa(len(ids)) + " chats?"
	}
	body := "Messages will be removed from this device and your other linked devices."
	if groups > 0 {
		body += " Groups aren't deleted: exit them from their menu first."
	}
	u.confirm(title, body, dialogButton{label: "Delete", primary: true, danger: true, run: func() {
		for _, id := range ids {
			u.deleteChat(id)
		}
		u.endChatSelect()
	}})
}

// pickMuteItems are the mute durations for the chats picked.
func (u *UI) pickMuteItems() []menuItem {
	mute := func(d time.Duration) func() {
		return func() {
			for _, c := range u.pickedChats() {
				u.backend.SetMuted(c.ID, true, d)
			}
			u.endChatSelect()
		}
	}
	return []menuItem{
		{key: "8h", label: "8 hours", run: mute(8 * time.Hour)},
		{key: "week", label: "1 week", run: mute(7 * 24 * time.Hour)},
		{key: "always", label: "Always", run: mute(0)},
		{divider: true},
		{note: true, label: "Muted chats still notify you when you're mentioned or replied to."},
	}
}

// layoutPickBar is select mode's bar, under the header: close, the count
// and, once a chat is picked, ⋮.
func (u *UI) layoutPickBar(gtx C) D {
	p := u.pal
	n := len(u.pickedChats())
	d := vcenter(gtx, gtx.Dp(58), func(gtx C) D {
		return layout.Inset{Left: 18, Right: 21}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return u.iconButton(gtx, u.btn("pick:cancel"), icClose, 40, 24, p.IconStrong) }),
				layout.Rigid(layout.Spacer{Width: 14}.Layout),
				layout.Flexed(1, u.label(16, itoa(n)+" selected", p.Text, labelOpts{maxLines: 1}).Layout),
				layout.Rigid(func(gtx C) D {
					if n == 0 {
						return D{}
					}
					return u.iconButton(gtx, u.btn("pick:menu"), icMenu, 40, 24, p.IconStrong)
				}),
			)
		})
	})
	line := max(1, gtx.Dp(1))
	fillRect(gtx, image.Rect(0, 0, d.Size.X, line), p.Divider)
	fillRect(gtx, image.Rect(0, d.Size.Y-line, d.Size.X, d.Size.Y), p.Divider)
	return d
}

// pickBox draws a row's checkbox at its left, sliding in with v (0 to 1).
// It's drawn over the row, whose own button opens the chat.
func (u *UI) pickBox(gtx C, c *widget.Clickable, id string, h int, v float32) {
	p := u.pal
	box, col := icCheckBoxEmpty, p.TextSecondary
	if u.sidebar.pick.picked[id] {
		box, col = icCheckBox, p.Green
	}
	sz := gtx.Dp(24)
	t := op.Offset(image.Pt(gtx.Dp(13+12)-int(float32(gtx.Dp(12))*(1-v)), (h-sz)/2)).Push(gtx.Ops)
	withOpacity(gtx, v, func() { drawIcon(gtx, box, 24, col) })
	t.Pop()
	if !u.sidebar.pick.on {
		return
	}
	// As wide as the space before the picture.
	defer op.Offset(image.Pt(gtx.Dp(13), 0)).Push(gtx.Ops).Pop()
	bgtx := gtx
	bgtx.Constraints = layout.Exact(image.Pt(gtx.Dp(48), h))
	clickable(bgtx, c, func(gtx C) D { return D{Size: gtx.Constraints.Max} })
}
