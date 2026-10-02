package ui

import (
	"image"
	"net/url"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"

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
		add(menuItem{key: "close", ic: icCancel, label: "Close chat", run: u.closeChat})
		return items
	}
	if c.IsGroup {
		if u.canAddMembers(c) {
			add(menuItem{key: "add", ic: icPersonAdd, label: "Add member", run: func() { u.openAddMembers(c) }})
		}
		add(menuItem{key: "info", ic: icInfo, label: "Group info", run: func() { u.openInfo(id) }})
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
	add(menuItem{key: "timer", glyph: disappearingIcon, label: "Disappearing messages", run: func() { u.openTimerMenu(c) }})
	add(menuItem{key: "theme", ic: icPalette, label: "Chat theme", run: func() { u.openChatTheme(c) }})
	if c.Favorite {
		add(menuItem{key: "fav", ic: icHeart, label: "Remove from favourites", run: func() { b.SetFavorite(id, false) }})
	} else {
		add(menuItem{key: "fav", ic: icHeart, label: "Add to favourites", run: func() { b.SetFavorite(id, true) }})
	}
	add(menuItem{key: "list", ic: icAddToList, label: "Add to list", run: func() { u.openListsMenu(c) }})
	add(menuItem{key: "export", ic: icDownload, label: "Export chat", run: func() { b.ExportChat(id) }})
	add(menuItem{key: "close", ic: icCancel, label: "Close chat", run: u.closeChat})
	add(menuItem{divider: true})
	if !c.IsGroup && !c.Self {
		add(menuItem{key: "report", ic: icThumbDown, label: "Report", run: u.reportUnsupported})
		add(menuItem{key: "block", ic: icBlock, label: "Block", run: func() { u.confirmBlock(id, c.Name) }})
	}
	add(menuItem{key: "clear", ic: icClear, label: "Clear chat", run: func() { u.confirmClearChat(id) }})
	if c.IsGroup {
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

// Chat themes: a color for your bubbles and the wallpaper, per chat.

type chatTheme struct {
	name string
	hue  uint32 // 0 for the default colors
}

var chatThemes = []chatTheme{
	{"Default", 0}, {"Teal", 0x00a884}, {"Blue", 0x3478f6}, {"Purple", 0x8e5bd8}, {"Pink", 0xd9468f},
	{"Red", 0xe0533d}, {"Orange", 0xf08c2e}, {"Yellow", 0xd6ad20}, {"Slate", 0x8696a0},
}

// prefChatTheme keys a chat's theme in Backend.Pref.
const prefChatTheme = "theme:"

// chatThemeOf returns the index of a chat's theme in chatThemes.
func (u *UI) chatThemeOf(id string) int {
	if u.themes == nil {
		u.themes = map[string]int{}
	}
	if i, ok := u.themes[id]; ok {
		return i
	}
	i := 0
	name := u.backend.Pref(prefChatTheme + id)
	for j, t := range chatThemes {
		if t.name == name {
			i = j
		}
	}
	u.themes[id] = i
	return i
}

func (u *UI) setChatTheme(id string, i int) {
	u.chatThemeOf(id)
	u.themes[id] = i
	name := chatThemes[i].name
	if i == 0 {
		name = ""
	}
	u.backend.SetPref(prefChatTheme+id, name)
}

// themed returns the palette with a theme's colors.
func themed(p *Palette, dark bool, t chatTheme) *Palette {
	if t.hue == 0 {
		return p
	}
	q := *p
	h := rgb(t.hue)
	if dark {
		q.BubbleOut = mix(rgb(0x1d1f1f), h, 0.42)
		q.QuoteOut = mix(q.BubbleOut, rgb(0x000000), 0.2)
		q.MetaOut = mix(q.BubbleOut, rgb(0xffffff), 0.62)
		q.SecondaryOut = q.MetaOut
		q.ChatBg = mix(p.ChatBg, h, 0.07)
		q.Doodle = mix(p.Doodle, h, 0.14)
	} else {
		q.BubbleOut = mix(rgb(0xffffff), h, 0.24)
		q.QuoteOut = mix(q.BubbleOut, h, 0.14)
		q.MetaOut = mix(q.BubbleOut, rgb(0x000000), 0.48)
		q.SecondaryOut = q.MetaOut
		q.ChatBg = mix(p.ChatBg, h, 0.12)
		q.Doodle = mix(p.Doodle, h, 0.16)
	}
	return &q
}

// chatPalette is the palette the open chat draws with.
func (u *UI) chatPalette(c *model.Chat) *Palette {
	if c == nil {
		return u.pal
	}
	return themed(u.pal, u.dark, chatThemes[u.chatThemeOf(c.ID)])
}

func (u *UI) openChatTheme(c *model.Chat) {
	u.dialog = dialogState{kind: dialogTheme, title: c.ID}
}

// themePanel picks the open chat's theme. A pick shows at once.
func (u *UI) themePanel(gtx C) D {
	d := &u.dialog
	p := u.pal
	id := d.title
	cur := u.chatThemeOf(id)
	for i := range chatThemes {
		if u.btn("theme:" + itoa(i)).Clicked(gtx) {
			u.setChatTheme(id, i)
			cur = i
		}
	}
	if u.btn("theme:done").Clicked(gtx) {
		u.closeDialog()
	}
	w := min(gtx.Dp(440), gtx.Constraints.Max.X-gtx.Dp(32))
	gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Constraints.Max.Y)}
	return layout.UniformInset(24).Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(u.label(19.5, "Chat theme", p.Text, labelOpts{weight: font.Medium}).Layout),
			layout.Rigid(layout.Spacer{Height: 6}.Layout),
			layout.Rigid(u.label(14.5, "Only you see the theme you choose.", p.TextSecondary).Layout),
			layout.Rigid(layout.Spacer{Height: 18}.Layout),
			layout.Rigid(func(gtx C) D {
				cols := 3
				cw := gtx.Constraints.Max.X / cols
				ch := gtx.Dp(96)
				for i, t := range chatThemes {
					x, y := (i%cols)*cw, (i/cols)*ch
					func() {
						defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
						bg := gtx
						bg.Constraints = layout.Exact(image.Pt(cw, ch))
						u.themeSwatch(bg, i, t, i == cur)
					}()
				}
				rows := (len(chatThemes) + cols - 1) / cols
				return D{Size: image.Pt(gtx.Constraints.Max.X, rows*ch)}
			}),
			layout.Rigid(layout.Spacer{Height: 16}.Layout),
			layout.Rigid(func(gtx C) D {
				return layout.E.Layout(gtx, func(gtx C) D {
					return u.dialogButton(gtx, "theme:done", dialogButton{label: "Done", primary: true})
				})
			}),
		)
	})
}

// themeSwatch is one theme: its wallpaper with a bubble of yours on it.
func (u *UI) themeSwatch(gtx C, i int, t chatTheme, on bool) D {
	p := u.pal
	tp := themed(p, u.dark, t)
	c := u.btn("theme:" + itoa(i))
	return clickable(gtx, c, func(gtx C) D {
		sz := gtx.Constraints.Max
		r := image.Rect(gtx.Dp(6), 0, sz.X-gtx.Dp(6), gtx.Dp(64))
		ring := p.PopupBorder
		if on {
			ring = p.Green
		} else if h := u.hover(gtx, c); h > 0 {
			ring = mix(ring, p.TextSecondary, h)
		}
		borderRRect(gtx, r, gtx.Dp(10), tp.ChatBg, ring)
		if on {
			borderRRect(gtx, r.Inset(1), gtx.Dp(10)-1, tp.ChatBg, ring)
		}
		b := image.Rect(r.Max.X-gtx.Dp(58), r.Min.Y+gtx.Dp(14), r.Max.X-gtx.Dp(12), r.Min.Y+gtx.Dp(32))
		fillRRect(gtx, b, gtx.Dp(6), tp.BubbleOut)
		in := image.Rect(r.Min.X+gtx.Dp(12), r.Min.Y+gtx.Dp(36), r.Min.X+gtx.Dp(52), r.Min.Y+gtx.Dp(52))
		fillRRect(gtx, in, gtx.Dp(6), tp.BubbleIn)
		l := record(gtx, u.label(13.5, t.name, p.Text, labelOpts{maxLines: 1, align: text.Middle}).Layout)
		l.at(gtx, (sz.X-l.size.X)/2, gtx.Dp(70))
		return D{Size: sz}
	})
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
