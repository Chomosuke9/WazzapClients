package ui

import (
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Chat actions shared by the chat list's menu and the info panels (and any
// other place that offers them): each asks first where WhatsApp does.

// muteStatus describes a muted chat's mute, or "" if it isn't muted.
func muteStatus(c *model.Chat) string {
	switch {
	case !c.Muted:
		return ""
	case c.MuteUntil.IsZero():
		return "Muted always"
	}
	return "Muted until " + c.MuteUntil.Format("02/01/2006 15:04")
}

// openMuteMenu opens the mute choices at the pointer.
func (u *UI) openMuteMenu(c *model.Chat) {
	u.ctx = ctxMenu{kind: ctxMute, chatID: c.ID, at: u.mouse}
}

// muteMenuItems are WhatsApp's mute durations, with the current choice
// ticked.
func (u *UI) muteMenuItems(c *model.Chat) []menuItem {
	b, id := u.backend, c.ID
	mute := func(d time.Duration) func() { return func() { b.SetMuted(id, true, d) } }
	always := c.Muted && c.MuteUntil.IsZero()
	items := []menuItem{
		{key: "all", label: "All messages", tick: !c.Muted, run: func() { b.SetMuted(id, false, 0) }},
	}
	if c.Muted && !always {
		items = append(items, menuItem{key: "until", label: muteStatus(c), tick: true})
	}
	return append(items,
		menuItem{key: "8h", label: "Mute for 8 hours", run: mute(8 * time.Hour)},
		menuItem{key: "week", label: "Mute for 1 week", run: mute(7 * 24 * time.Hour)},
		menuItem{key: "always", label: "Mute always", tick: always, run: mute(0)},
		menuItem{divider: true},
		menuItem{note: true, label: "Muted chats still notify you when you're mentioned or replied to."},
	)
}

// openListsMenu opens the "Add to list" checkboxes at the pointer. It stays
// open while boxes are ticked.
func (u *UI) openListsMenu(c *model.Chat) {
	u.ctx = ctxMenu{kind: ctxLists, chatID: c.ID, at: u.mouse, all: u.backend.Lists()}
}

func (u *UI) confirmClearChat(id string) {
	u.confirm("Clear this chat?", "Messages will only be removed from this device and your devices on the newer versions of WhatsApp.",
		dialogButton{label: "Clear chat", primary: true, run: func() { u.backend.ClearChat(id) }})
}

func (u *UI) confirmExitGroup(id, name string) {
	u.confirm("Exit \""+name+"\"?", "Only group admins will be notified that you left the group.",
		dialogButton{label: "Exit group", primary: true, danger: true, run: func() { u.backend.LeaveGroup(id) }})
}

func (u *UI) confirmDeleteChat(id string) {
	u.confirm("Delete this chat?", "Messages will be removed from this device and your other linked devices.",
		dialogButton{label: "Delete chat", primary: true, danger: true, run: func() {
			if u.selected != nil && u.selected.ID == id {
				u.closeChat()
			}
			u.backend.DeleteChat(id)
		}})
}

// reportUnsupported answers the Report actions: reports go to Meta, which
// only the phone app can reach.
func (u *UI) reportUnsupported() {
	u.toast("Reporting isn't supported in this app yet. Report from your phone.")
}
