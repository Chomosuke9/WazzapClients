package ui

import (
	"image"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// Select chats (the chat list's ⋮ menu): checkboxes on the rows, and a bar
// in place of the list's header that archives, mutes, pins, marks read or
// unread, or deletes the chats picked, like WhatsApp's.

// chatPick is the chat list's select mode.
type chatPick struct {
	on     bool
	picked map[string]bool
	anim   tween // the checkboxes sliding in
}

func (u *UI) startChatSelect() {
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

// pickActions runs the select bar's buttons.
func (u *UI) pickActions(gtx C) {
	if u.btn("pick:cancel").Clicked(gtx) {
		u.endChatSelect()
	}
	for _, a := range []string{"archive", "mute", "pin", "read", "delete"} {
		if u.btn("pick:" + a).Clicked(gtx) {
			u.pickAction(a)
		}
	}
}

// pickAction runs a select bar action on the chats picked. Each one but
// a mute's durations and a delete's confirmation ends select mode, as in
// WhatsApp.
func (u *UI) pickAction(a string) {
	chats := u.pickedChats()
	if len(chats) == 0 {
		return
	}
	b := u.backend
	all := func(f func(c *model.Chat) bool) bool {
		for _, c := range chats {
			if !f(c) {
				return false
			}
		}
		return true
	}
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
		{key: "8h", label: "Mute for 8 hours", run: mute(8 * time.Hour)},
		{key: "week", label: "Mute for 1 week", run: mute(7 * 24 * time.Hour)},
		{key: "always", label: "Mute always", run: mute(0)},
		{divider: true},
		{note: true, label: "Muted chats still notify you when you're mentioned or replied to."},
	}
}

// layoutPickBar replaces the chat list's header in select mode: close,
// the count, then the actions, dimmed until a chat is picked.
func (u *UI) layoutPickBar(gtx C) D {
	p := u.pal
	chats := u.pickedChats()
	col := p.IconStrong
	if len(chats) == 0 {
		col = p.EmptyIcon
	}
	readIc := icMarkUnread
	if len(chats) == 0 || u.pickedUnread(chats) {
		readIc = icon.DoneAll
	}
	btn := func(key string, ic *icon.Icon) layout.FlexChild {
		return layout.Rigid(func(gtx C) D { return u.iconButton(gtx, u.btn(key), ic, 38, 22, col) })
	}
	return vcenter(gtx, gtx.Dp(68), func(gtx C) D {
		return layout.Inset{Left: 10, Right: 12}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return u.iconButton(gtx, u.btn("pick:cancel"), icClose, 40, 24, p.IconStrong) }),
				layout.Rigid(layout.Spacer{Width: 10}.Layout),
				layout.Flexed(1, u.label(17, itoa(len(chats))+" selected", p.Text, labelOpts{weight: font.Medium, maxLines: 1}).Layout),
				btn("pick:archive", icArchive),
				btn("pick:mute", icMuted),
				btn("pick:pin", icPin),
				btn("pick:read", readIc),
				btn("pick:delete", icDelete),
			)
		})
	})
}

// pickBox draws a row's checkbox at its left, sliding in with v (0 to 1).
func (u *UI) pickBox(gtx C, id string, h int, v float32) {
	p := u.pal
	box, col := icCheckBoxEmpty, p.TextSecondary
	if u.sidebar.pick.picked[id] {
		box, col = icCheckBox, p.Green
	}
	sz := gtx.Dp(24)
	t := op.Offset(image.Pt(gtx.Dp(13+12)-int(float32(gtx.Dp(12))*(1-v)), (h-sz)/2)).Push(gtx.Ops)
	withOpacity(gtx, v, func() { drawIcon(gtx, box, 24, col) })
	t.Pop()
}
