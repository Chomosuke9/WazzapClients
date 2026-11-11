package ui

import (
	"net/url"
	"strings"
	"time"

	"gioui.org/layout"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// The open chat's ⋮ menu, and the chat settings it shares with the info
// panel: disappearing messages, the chat's theme, its encryption, adding
// members and the group's invite link.

// openConvMenu opens the conversation header's ⋮ menu at the pointer.
func (u *UI) openConvMenu() {
	if c := u.selected; c != nil {
		u.ctx = ctxMenu{kind: ctxConv, chatID: c.ID, at: u.mouse}
	}
}

// convMenuItems mirrors WhatsApp's ⋮ menu of an open chat.
func (u *UI) convMenuItems(c *model.Chat) []menuItem {
	b, id := u.backend, c.ID
	var items []menuItem
	add := func(it menuItem) { items = append(items, it) }
	if isChannelID(id) {
		if ch := u.channelByID(id); ch != nil {
			return u.channelMenuItems(ch, true)
		}
		add(menuItem{key: "close", ic: icCancel, label: "Close channel", run: u.closeChat})
		return items
	}
	if c.IsGroup {
		if u.canAddMembers(c) && !c.Left {
			add(menuItem{key: "add", ic: icPersonAdd, label: "Add member", run: func() { u.openAddMembers(c) }})
		}
		add(menuItem{key: "info", ic: icInfo, label: "Group info", run: func() { u.openInfo(id) }})
		if u.extendedMentions && !c.Left {
			add(menuItem{key: "mentions", ic: icGroups, label: "Custom mentions", run: func() {
				u.openInfo(id)
				u.openInfoSub(infoMentions)
			}})
		}
	} else {
		add(menuItem{key: "info", ic: icInfo, label: "Contact info", run: func() { u.openInfo(id) }})
	}
	add(menuItem{key: "search", ic: icSearch, label: "Search", run: u.openChatSearch})
	add(menuItem{key: "select", ic: icCheckBox, label: "Select messages", run: u.startSelectNone})
	if c.Muted {
		add(menuItem{key: "mute", ic: icBellLine, label: "Unmute notifications", sub: muteStatus(c), run: func() { b.SetMuted(id, false, 0) }})
	} else {
		add(menuItem{key: "mute", ic: icMuted, label: "Mute notifications", run: func() { u.openMuteMenu(c) }})
	}
	if !c.Left {
		add(menuItem{key: "timer", glyph: disappearingIcon, label: "Disappearing messages", run: func() { u.openTimerMenu(c) }})
	}
	add(menuItem{key: "theme", ic: icPalette, label: "Chat theme", run: func() { u.openChatTheme(c) }})
	if c.Favorite {
		add(menuItem{key: "fav", ic: icHeart, label: "Remove from favourites", run: func() { b.SetFavorite(id, false) }})
	} else {
		add(menuItem{key: "fav", ic: icHeart, label: "Add to favourites", run: func() { b.SetFavorite(id, true) }})
	}
	add(menuItem{key: "list", ic: icAddToList, label: "Add to list", run: func() { u.openListsMenu(c) }})
	add(menuItem{key: "export", ic: icDownload, label: "Export chat", run: func() { u.exportChat(id) }})
	add(menuItem{key: "close", ic: icCancel, label: "Close chat", run: u.closeChat})
	add(menuItem{divider: true})
	if !c.IsGroup && !c.Self {
		add(menuItem{key: "report", ic: icThumbDown, label: "Report", run: u.reportUnsupported})
		add(menuItem{key: "block", ic: icBlock, label: "Block", run: func() { u.confirmBlock(id, c.Name) }})
	}
	add(menuItem{key: "clear", ic: icClear, label: "Clear chat", run: func() { u.confirmClearChat(id) }})
	if c.IsGroup && c.Left {
		add(menuItem{key: "delete", ic: icDelete, label: "Delete group", run: func() { u.confirmDeleteGroup(id, c.Name) }})
	} else if c.IsGroup {
		add(menuItem{key: "exit", ic: icLogout, label: "Exit group", run: func() { u.confirmExitGroup(id, c.Name) }})
	} else {
		add(menuItem{key: "delete", ic: icDelete, label: "Delete chat", run: func() { u.confirmDeleteChat(id) }})
	}
	return items
}

// startSelectNone starts select mode with nothing picked yet.
func (u *UI) startSelectNone() {
	u.conv.selecting = true
	u.conv.picked = map[string]bool{}
}

func (u *UI) confirmBlock(id, name string) {
	u.confirm("Block "+name+"?", "Blocked contacts will no longer be able to call you or send you messages.",
		dialogButton{label: "Block", primary: true, danger: true, run: func() { u.backend.SetBlocked(id, true) }})
}

// callsUnsupported answers the call buttons.
func (u *UI) callsUnsupported() {
	u.toast("Calls aren't supported in this app yet. Call from your phone.")
}

// inform shows a notice with a single OK.
func (u *UI) inform(title, body string) {
	u.dialog = dialogState{kind: dialogConfirm, title: title, body: body,
		buttons: []dialogButton{{label: "OK", primary: true}}}
}

// Disappearing messages.

// timerChoices are WhatsApp's disappearing message timers.
var timerChoices = []struct {
	label string
	d     time.Duration
}{
	{"Off", 0}, {"24 hours", 24 * time.Hour}, {"7 days", 7 * 24 * time.Hour}, {"90 days", 90 * 24 * time.Hour},
}

// openTimerMenu opens the disappearing message timers at the pointer.
func (u *UI) openTimerMenu(c *model.Chat) {
	if c.IsGroup {
		if info := u.backend.Info(c.ID); info != nil && info.Locked && len(info.Members) > 0 && !u.isAdminIn(info) {
			u.inform("Only admins can change this", "Only group admins can change disappearing messages in this group.")
			return
		}
	}
	u.ctx = ctxMenu{kind: ctxTimer, chatID: c.ID, at: u.mouse}
}

// isAdminIn reports whether you administer the group info describes.
func (u *UI) isAdminIn(info *model.ChatInfo) bool {
	for _, m := range info.Members {
		if m.Me {
			return m.Admin
		}
	}
	return false
}

// timerItems lists the timers with the chat's ticked.
func (u *UI) timerItems(c *model.Chat) []menuItem {
	var cur uint32
	if info := u.timerInfo(c.ID); info != nil {
		cur = info.Disappearing
	}
	items := []menuItem{{note: true, label: "New messages will disappear from this chat after the selected duration."}}
	for _, t := range timerChoices {
		d := t.d
		items = append(items, menuItem{key: t.label, label: t.label, tick: uint32(d/time.Second) == cur,
			run: func() { u.backend.SetDisappearing(c.ID, d) }})
	}
	return items
}

// timerInfo is the chat's info for its timer: the open panel's, or the
// one the composer loaded.
func (u *UI) timerInfo(id string) *model.ChatInfo {
	if u.info.chatID == id && u.info.data != nil {
		return u.info.data
	}
	return u.chatMembers(id)
}

// Encryption.

// secCode is the security code being shown, or fetched.
type secCode struct {
	chatID, code, err string
}

// openEncryption explains a chat's encryption; a contact's also shows
// the security code to compare with theirs.
func (u *UI) openEncryption(c *model.Chat, name string) {
	if c.IsGroup {
		u.inform("Encryption", "Messages and calls in this group are end-to-end encrypted. "+
			"No one outside of it, not even WhatsApp, can read or listen to them.")
		return
	}
	u.secCode = secCode{chatID: c.ID}
	u.backend.SecurityCode(c.ID)
	id := c.ID
	u.dialog = dialogState{kind: dialogConfirm, title: "Verify security code",
		bodyFn: func() string {
			s := u.secCode
			intro := "Messages and calls with " + name + " are end-to-end encrypted. To verify it, compare this code with the one on their phone.\n\n"
			switch {
			case s.chatID != id:
				return intro
			case s.err != "":
				return intro + s.err
			case s.code == "":
				return intro + "Getting the security code…"
			}
			return intro + groupDigits(s.code)
		},
		buttons: []dialogButton{
			{label: "Copy code", primary: true, run: func() {
				if u.secCode.chatID == id && u.secCode.code != "" {
					u.pendingCopy = u.secCode.code
					u.toast("Code copied")
				}
			}},
			{label: "Close"},
		},
	}
}

// groupDigits spaces a security code in groups of five, four to a line.
func groupDigits(code string) string {
	var b strings.Builder
	for i := 0; i < len(code); i += 5 {
		if i > 0 {
			if i%20 == 0 {
				b.WriteByte('\n')
			} else {
				b.WriteString("   ")
			}
		}
		b.WriteString(code[i:min(len(code), i+5)])
	}
	return b.String()
}

// Members and invites.

// canAddMembers reports whether you may add people to a group.
func (u *UI) canAddMembers(c *model.Chat) bool {
	if !c.IsGroup || u.announcementsOf(c) != nil {
		return false
	}
	info := u.timerInfo(c.ID)
	if info == nil || len(info.Members) == 0 {
		return false
	}
	return u.isAdminIn(info) || !info.AdminsAdd
}

// openAddMembers picks people to add to a group.
func (u *UI) openAddMembers(c *model.Chat) {
	u.dialog = dialogState{kind: dialogForward, contacts: true, addTo: c.ID}
	u.dialog.search.SingleLine = true
	u.dialog.list.Axis = layout.Vertical
	if info := u.timerInfo(c.ID); info != nil {
		for _, m := range info.Members {
			u.dialog.members = append(u.dialog.members, m.ID)
		}
	}
	u.requestFocus(&u.dialog.search)
}

// addMembers adds the picked people and says how it went.
func (u *UI) addMembers(chat string, ids []string) {
	slashHost{u: u}.Group(model.GroupRequest{ChatID: chat, Action: model.GroupAdd, Members: ids}, func(e model.GroupEvent) {
		if e.Err != "" {
			u.toast(e.Err)
			return
		}
		added, failed := 0, []string{}
		for _, r := range e.Members {
			if r.Err == "" {
				added++
			} else {
				failed = append(failed, r.Name)
			}
		}
		switch {
		case len(failed) == 0 && added == 1:
			u.toast("Added 1 member")
		case len(failed) == 0:
			u.toast("Added " + itoa(added) + " members")
		default:
			u.toast("Couldn't add " + strings.Join(failed, ", ") + ". Invite them with the group link instead.")
		}
	})
}

// groupLink is the invite link the link dialog shows.
type groupLink struct {
	chatID, link, err string
}

// fetchGroupLink asks for a group's invite link (a new one when reset),
// and runs then with it.
func (u *UI) fetchGroupLink(chat string, reset bool, then func(link string)) {
	u.link = groupLink{chatID: chat}
	slashHost{u: u}.Group(model.GroupRequest{ChatID: chat, Action: model.GroupLink, On: reset}, func(e model.GroupEvent) {
		if u.link.chatID != chat {
			return
		}
		u.link.link, u.link.err = e.Link, e.Err
		if e.Err == "" && then != nil {
			then(e.Link)
		}
	})
}

// openInviteLink shows the group's invite link, to copy or reset.
func (u *UI) openInviteLink(chat string) {
	u.fetchGroupLink(chat, false, nil)
	u.dialog = dialogState{kind: dialogConfirm, title: "Invite to group via link",
		bodyFn: func() string {
			l := u.link
			switch {
			case l.err != "":
				return l.err
			case l.link == "":
				return "Getting the link…"
			}
			return "Anyone with WhatsApp can follow this link to join this group. Only share it with people you trust.\n\n" + l.link
		},
		buttons: []dialogButton{
			{label: "Copy link", primary: true, run: func() {
				if u.link.chatID == chat && u.link.link != "" {
					u.pendingCopy = u.link.link
					u.toast("Link copied")
				}
			}},
			{label: "Reset link", danger: true, run: func() {
				u.confirm("Reset this link?", "The current link will stop working and a new one will be made.",
					dialogButton{label: "Reset link", primary: true, danger: true, run: func() {
						u.fetchGroupLink(chat, true, func(string) { u.openInviteLinkShown(chat) })
					}})
			}},
			{label: "Close"},
		},
	}
}

// openInviteLinkShown shows the link dialog again, without asking for the
// link a second time.
func (u *UI) openInviteLinkShown(chat string) {
	l := u.link
	u.openInviteLink(chat)
	u.link = l
}

// inviteByEmail opens a new email with the group's invite link.
func (u *UI) inviteByEmail(chat, name string) {
	u.toast("Getting the link…")
	u.fetchGroupLink(chat, false, func(link string) {
		if !openMail("Join my WhatsApp group", "Follow this link to join my WhatsApp group \""+name+"\": "+link) {
			u.toast("Couldn't open your email app.")
		}
	})
}

// openMail opens the default email app with a new message.
func openMail(subject, body string) bool {
	return openTarget("mailto:?subject=" + url.PathEscape(subject) + "&body=" + url.PathEscape(body))
}
