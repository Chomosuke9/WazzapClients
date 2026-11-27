package ui

import (
	"image"
	"image/color"
	"slices"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
	"github.com/chomosuke9/wazzapclients/internal/ui/styledtext"
)

// Pages that open inside the info panel, with a back arrow: a chat's
// starred messages, its media (infomedia.go), its disappearing messages timer,
// Restricted chat, a group's permissions and admins, its member changes and
// its custom mentions and the people of a group mention in a message
// (custommention.go).

const (
	infoStarred     = "starred"
	infoGallery     = "chatgallery" // the chat's media, links and docs
	infoTimer       = "timer"
	infoRestricted  = "restricted"
	infoPerms       = "perms"
	infoAdmins      = "admins" // Edit group admins, opened from Group permissions
	infoChanges     = "changes"
	infoMentions    = "mentions"
	infoMentioned   = "mentioned"   // whom a message's @admin or custom mention notifies
	infoTranscripts = "transcripts" // the chat's transcript language (transcripts.go)
)

// openInfoSub shows one of the panel's pages.
func (u *UI) openInfoSub(sub string) {
	if u.info.sub == "" {
		u.info.subPos = u.info.list.Position
	}
	u.info.sub = sub
	u.info.list.Position = layout.Position{}
	switch sub {
	case infoStarred:
		u.info.starred.start(u.backend, model.GalleryQuery{Kind: model.GalleryStarred, ChatID: u.info.chatID, Limit: 50})
	case infoGallery:
		u.openInfoGallery()
	case infoAdmins:
		u.info.admins = nil
	case infoChanges:
		u.info.changes = u.backend.MemberChanges(u.info.chatID)
	}
}

// closeInfoSub returns to the panel's main page, or from Edit group admins
// to the permissions.
func (u *UI) closeInfoSub() {
	if u.info.sub == infoAdmins {
		u.openInfoSub(infoPerms)
		return
	}
	u.leaveInfoSub()
	u.info.list.Position = u.info.subPos
}

// leaveInfoSub puts the panel's page away, letting go of its messages.
func (u *UI) leaveInfoSub() {
	u.info.sub = ""
	u.info.starred, u.info.gallery = galleryList{}, galleryState{}
}

func infoSubTitle(sub string) string {
	switch sub {
	case infoStarred:
		return "Starred messages"
	case infoTimer:
		return "Disappearing messages"
	case infoRestricted:
		return "Restricted chat"
	case infoPerms:
		return "Group permissions"
	case infoAdmins:
		return "Edit group admins"
	case infoChanges:
		return "Member changes"
	case infoMentions:
		return "Custom mentions"
	case infoTranscripts:
		return "Chat transcript language"
	}
	return ""
}

// infoSubRows builds a page's rows.
func (u *UI) infoSubRows(gtx C, c *model.Chat, info *model.ChatInfo) []layout.Widget {
	switch u.info.sub {
	case infoStarred:
		return u.starredRows(gtx)
	case infoTimer:
		return u.timerRows(gtx, c, info)
	case infoRestricted:
		return u.restrictedRows(info)
	case infoPerms:
		return u.permRows(gtx, c, info)
	case infoAdmins:
		return u.adminRows(gtx, info)
	case infoChanges:
		return u.changeRows()
	case infoMentions:
		return u.customMentionPage(gtx, c, info)
	case infoMentioned:
		return u.mentionPeopleRows(gtx, info)
	case infoTranscripts:
		return u.transcriptLangRows(gtx, c)
	}
	return nil
}

// galleryList is a list of messages that Backend.Gallery fills, a page at
// a time.
type galleryList struct {
	q       model.GalleryQuery // the query, at offset 0
	pending model.GalleryQuery // the page asked for
	msgs    []*model.Message
	more    bool
	loading bool
}

// start asks for the first page of q.
func (g *galleryList) start(b model.Backend, q model.GalleryQuery) {
	*g = galleryList{q: q, pending: q, loading: true}
	b.Gallery(q)
}

// next asks for the page after the loaded ones.
func (g *galleryList) next(b model.Backend) {
	if !g.more || g.loading {
		return
	}
	g.pending = g.q
	g.pending.Offset = len(g.msgs)
	g.loading = true
	b.Gallery(g.pending)
}

// loaded takes a page in, if it's the one asked for.
func (g *galleryList) loaded(e model.GalleryEvent) bool {
	if !g.loading || e.Query != g.pending {
		return false
	}
	g.msgs = append(g.msgs, e.Msgs...)
	g.more, g.loading = e.More, false
	return true
}

// galleryLoaded hands a page to the list that asked for it.
func (u *UI) galleryLoaded(e model.GalleryEvent) {
	if !u.gallery.list.loaded(e) && !u.info.gallery.list.loaded(e) {
		u.info.starred.loaded(e)
	}
}

// starredRows lists the chat's starred messages; a click shows one in
// the chat.
func (u *UI) starredRows(gtx C) []layout.Widget {
	p := u.pal
	s := &u.info.starred
	if len(s.msgs) == 0 {
		txt := "No starred messages"
		if s.loading {
			txt = "Loading…"
		}
		return []layout.Widget{func(gtx C) D {
			return layout.Inset{Left: infoPadX, Right: 24, Top: 40}.Layout(gtx, func(gtx C) D {
				return layout.N.Layout(gtx, u.label(15, txt, p.TextSecondary).Layout)
			})
		}}
	}
	var rows []layout.Widget
	for i, m := range s.msgs {
		if i == len(s.msgs)-1 {
			s.next(u.backend) // the last row is in the list: load more
		}
		rows = append(rows, func(gtx C) D { return u.starredRow(gtx, m, false) })
	}
	return rows
}

// starredRow is a starred message: who sent it, and the message in a
// bubble. In the Media panel's starred messages from every chat (all), it
// also names the chat.
func (u *UI) starredRow(gtx C, m *model.Message, all bool) D {
	p := u.pal
	c := u.btn("starred:" + m.ChatID + "/" + m.ID)
	if c.Clicked(gtx) {
		u.showMessage(m)
	}
	who := m.Sender
	if m.FromMe {
		who = "You"
	}
	if who == "" {
		if ch := u.chatByID(m.ChatID); ch != nil {
			who = ch.Name
		}
	}
	who = plainText(who)
	padX, bubW := unit.Dp(infoPadX), gtx.Constraints.Max.X
	if all {
		if ch := u.chatByID(m.ChatID); ch != nil && ch.Name != who {
			who += " · " + plainText(ch.Name)
		}
		padX, bubW = 30, gtx.Dp(640)
	}
	txt := plainText(m.Text)
	if m.Media != model.MediaNone {
		txt = mediaLabel(m)
	}
	return clickable(gtx, c, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		bg := mix(p.Panel, p.Hover, u.hover(gtx, c))
		return background(gtx, bg, 0, func(gtx C) D {
			return layout.Inset{Left: padX, Right: 24, Top: 12, Bottom: 12}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return u.avatar(gtx, m.SenderID, who, false, 24) }),
							layout.Rigid(layout.Spacer{Width: 10}.Layout),
							layout.Flexed(1, u.label(14.5, who, p.Text, labelOpts{weight: font.Medium, maxLines: 1}).Layout),
							layout.Rigid(u.label(13, listTime(m.Time, u.now()), p.TextSecondary, labelOpts{maxLines: 1}).Layout),
						)
					}),
					layout.Rigid(layout.Spacer{Height: 8}.Layout),
					layout.Rigid(func(gtx C) D {
						bub := p.BubbleIn
						if m.FromMe {
							bub = p.BubbleOut
						}
						gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, bubW)
						l := record(gtx, func(gtx C) D {
							return layout.Inset{Left: 9, Right: 9, Top: 6, Bottom: 7}.Layout(gtx, func(gtx C) D {
								return layout.Flex{Alignment: layout.End}.Layout(gtx,
									layout.Flexed(1, u.label(14.5, txt, p.Text, labelOpts{maxLines: 4}).Layout),
									layout.Rigid(layout.Spacer{Width: 8}.Layout),
									layout.Rigid(iconW(icStarFill, 14, p.TextSecondary)),
								)
							})
						})
						fillRRect(gtx, image.Rectangle{Max: l.size}, gtx.Dp(8), bub)
						l.at(gtx, 0, 0)
						return D{Size: l.size}
					}),
				)
			})
		})
	})
}

// showMessage opens a message's chat and scrolls to it.
func (u *UI) showMessage(m *model.Message) {
	u.closeGallery()
	if u.selected == nil || u.selected.ID != m.ChatID {
		c := u.chatByID(m.ChatID)
		if c == nil {
			u.toast("That chat isn't in your chat list.")
			return
		}
		u.setPage(pageChats)
		u.open(c)
	}
	u.jumpTo(m.ID)
}

// permRows are a group's permissions, as WhatsApp lists them. Only admins
// see the page.
func (u *UI) permRows(gtx C, c *model.Chat, info *model.ChatInfo) []layout.Widget {
	id := c.ID
	set := func(a model.GroupAction, on bool) func() {
		return func() {
			slashHost{u: u}.Group(model.GroupRequest{ChatID: id, Action: a, On: on}, func(e model.GroupEvent) {
				if e.Err != "" {
					u.toast(e.Err)
				}
			})
		}
	}
	if u.btn("info:admins").Clicked(gtx) {
		u.openInfoSub(infoAdmins)
	}
	return []layout.Widget{
		u.infoSection("Members can:", false),
		u.permRow(id, "edit", icEdit, "Edit group settings",
			"This includes the name, icon, description, disappearing message timer, and the ability to pin, keep or unkeep messages.",
			!info.Locked, set(model.GroupLock, !info.Locked)),
		u.permRow(id, "send", icChats, "Send new messages", "", !info.Announce, set(model.GroupAnnounce, !info.Announce)),
		u.permRow(id, "addmembers", icPersonAdd, "Add other members", "", !info.AdminsAdd, set(model.GroupAddMode, !info.AdminsAdd)),
		u.infoSection("Admins can:", false),
		u.permRow(id, "approve", icPersonAddFill, "Approve new members",
			"When turned on, admins must approve anyone who wants to join this group.",
			info.Approval, set(model.GroupApproval, !info.Approval)),
		u.infoSection("Group admins", false),
		u.infoItem("admins", listItem{ic: icGroupAdmins, title: "Edit group admins", sub: adminNames(info)}),
	}
}

// permRow is a permission of group id with its switch.
func (u *UI) permRow(id, key string, ic *icon.Icon, title, sub string, on bool, run func()) layout.Widget {
	return func(gtx C) D {
		b := u.btn("perm:" + key)
		if b.Clicked(gtx) {
			run()
		}
		g := infoGeom
		g.subHeight, g.padRight = 72, 0 // the switch keeps the text off the edge
		return u.layoutListItem(gtx, b, listItem{ic: ic, title: title, sub: sub,
			trailing: u.infoSwitch("perm:"+key+":"+id, on)}, g)
	}
}

// adminNames lists a group's admins.
func adminNames(info *model.ChatInfo) string {
	var names []string
	for _, m := range info.Members {
		switch {
		case m.Me && m.Admin:
			names = append(names, "You")
		case m.Admin:
			names = append(names, plainText(memberTitle(m)))
		}
	}
	return strings.Join(names, ", ")
}

// adminRows are a group's members with a box each, checked for its admins.
// Nothing changes until it's saved (adminsFooter). You and the group's
// creator stay admins.
func (u *UI) adminRows(gtx C, info *model.ChatInfo) []layout.Widget {
	var rows []layout.Widget
	for _, m := range info.Members {
		on, picked := u.info.admins[m.ID]
		if !picked {
			on = m.Admin
		}
		fixed := m.Me || m.SuperAdmin
		b := u.btn("admin:" + m.ID)
		if b.Clicked(gtx) && !fixed {
			if u.info.admins == nil {
				u.info.admins = map[string]bool{}
			}
			if on = !on; on == m.Admin {
				delete(u.info.admins, m.ID)
			} else {
				u.info.admins[m.ID] = on
			}
		}
		title := memberTitle(m)
		if m.Me {
			title = "You"
		}
		rows = append(rows, func(gtx C) D {
			box, col := icCheckBoxEmpty, u.pal.TextSecondary
			if on {
				box, col = icCheckBox, u.pal.Green
			}
			if fixed {
				col = u.pal.TextSecondary
				gtx = gtx.Disabled()
			}
			g := infoGeom
			g.iconCenter = infoPadX - g.hoverLeft + 25.5
			g.textLeft = 75.4
			g.height, g.subHeight = 76, 76
			return u.layoutListItem(gtx, b, listItem{title: title, sub: memberSub(m),
				glyph:    func(gtx C, _ color.NRGBA) D { return u.memberAvatar(gtx, m) },
				trailing: func(gtx C) D { return layout.Inset{Left: 12, Right: 12}.Layout(gtx, iconW(box, 24, col)) }}, g)
		})
	}
	return rows
}

// adminsFooter is Edit group admins' Save button, once something changed.
func (u *UI) adminsFooter(gtx C, c *model.Chat) D {
	if len(u.info.admins) == 0 {
		return D{}
	}
	if u.btn("admins:save").Clicked(gtx) {
		u.saveAdmins(c.ID)
	}
	return layout.Inset{Left: infoPadX, Right: infoPadX, Top: 12, Bottom: 16}.Layout(gtx, func(gtx C) D {
		return u.wideButton(gtx, "admins:save", "Save", false)
	})
}

// saveAdmins makes the members picked on Edit group admins admins, or
// dismisses them, and returns to the permissions.
func (u *UI) saveAdmins(id string) {
	change := map[model.GroupAction][]string{}
	for m, on := range u.info.admins {
		a := model.GroupDemote
		if on {
			a = model.GroupPromote
		}
		change[a] = append(change[a], m)
	}
	for a, ids := range change {
		slices.Sort(ids)
		slashHost{u: u}.Group(model.GroupRequest{ChatID: id, Action: a, Members: ids}, func(e model.GroupEvent) {
			if e.Err != "" {
				u.toast(e.Err)
				return
			}
			var failed []string
			for _, r := range e.Members {
				if r.Err != "" {
					failed = append(failed, r.Name)
				}
			}
			if len(failed) > 0 {
				u.toast("Couldn't change " + strings.Join(failed, ", ") + ".")
			}
		})
	}
	u.openInfoSub(infoPerms)
}

// timerRows are a chat's disappearing message timers, and what they do.
func (u *UI) timerRows(gtx C, c *model.Chat, info *model.ChatInfo) []layout.Widget {
	p := u.pal
	who := "Anyone in the chat can change this setting."
	if info.IsGroup && info.Locked {
		who = "Only group admins can change this setting."
	}
	rows := []layout.Widget{
		u.infoSection("Make messages in this chat disappear", false),
		u.infoText("For more privacy and storage, all new messages will disappear from this chat for everyone "+
			"after the selected duration, except when kept. "+who, p.TextSecondary, 14, 0, 14, infoPadX),
	}
	id := c.ID
	for _, t := range timerChoices {
		on := uint32(t.d/time.Second) == info.Disappearing
		b := u.btn("info:timer:" + t.label)
		if b.Clicked(gtx) && !on {
			u.backend.SetDisappearing(id, t.d)
		}
		rows = append(rows, func(gtx C) D {
			return u.layoutListItem(gtx, b, listItem{title: t.label,
				glyph: func(gtx C, _ color.NRGBA) D { return u.radio(gtx, on) }}, infoGeom)
		})
	}
	if info.IsGroup {
		return rows // the default timer is for one-to-one chats
	}
	link := u.btn("info:timer:default")
	if link.Clicked(gtx) {
		u.setPage(pageSettings)
		u.openSettings(settingPrivacy)
		u.openSettingsSub("timer")
	}
	return append(rows, func(gtx C) D {
		return layout.Inset{Left: infoPadX, Right: 24, Top: 14, Bottom: 24}.Layout(gtx, func(gtx C) D {
			return clickable(gtx, link, func(gtx C) D {
				f, bold := font.Font{Typeface: u.th.Face}, font.Font{Typeface: u.th.Face, Weight: font.SemiBold}
				return u.layoutSpansLH(gtx, []styledtext.SpanStyle{
					{Font: f, Size: 14, Color: p.TextSecondary, Content: "Update your "},
					{Font: bold, Size: 14, Color: p.Green, Content: "default message timer"},
					{Font: f, Size: 14, Color: p.TextSecondary, Content: " in Settings"},
				}, nil, 20, nil, nil)
			})
		})
	})
}

// restrictedRows explain Restricted chat, whose switch only shows whether
// it's on: hypermeow has no way to change it (WhatsApp sends it as a
// LIMIT_SHARING protocol message, and for groups a group setting), and
// WhatsApp's own linked devices leave it to the phone.
func (u *UI) restrictedRows(info *model.ChatInfo) []layout.Widget {
	p := u.pal
	point := func(ic *icon.Icon, txt string) layout.Widget {
		return func(gtx C) D {
			return layout.Inset{Left: infoPadX, Right: 24, Top: 10, Bottom: 10}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(iconW(ic, 24, p.TextSecondary)),
					layout.Rigid(layout.Spacer{Width: 20}.Layout),
					layout.Flexed(1, u.infoPara(txt, p.Text, 15)),
				)
			})
		}
	}
	para := func(txt string, col color.NRGBA, size unit.Sp, top, bottom unit.Dp) layout.Widget {
		return func(gtx C) D {
			return layout.Inset{Left: infoPadX, Right: 24, Top: top, Bottom: bottom}.Layout(gtx, u.infoPara(txt, col, size))
		}
	}
	return []layout.Widget{
		para("Limit what people in this chat can do with the messages", p.Text, 17, 10, 10),
		para("For your most sensitive chats, you can turn off some standard WhatsApp features.", p.TextSecondary, 14, 0, 14),
		u.infoDivider(0, 14),
		func(gtx C) D {
			return layout.Inset{Left: infoPadX, Right: 30, Bottom: 16}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(u.label(16, "Restricted chat", p.Text, labelOpts{maxLines: 1}).Layout),
							layout.Rigid(layout.Spacer{Height: 3}.Layout),
							layout.Rigid(u.infoPara("Change this setting on your primary phone.", p.TextSecondary, 14)),
						)
					}),
					layout.Rigid(layout.Spacer{Width: 12}.Layout),
					layout.Rigid(func(gtx C) D {
						// Grayed: it can't be turned here.
						defer paint.PushOpacity(gtx.Ops, 0.3).Pop()
						return u.themeSwitch(gtx, "info:restricted:"+info.ID, info.LimitSharing)
					}),
				)
			})
		},
		u.infoDivider(0, 6),
		u.infoSection("If this is on, people:", false),
		point(icImage, "Can't save media from this chat to their device gallery automatically"),
		point(icShare, "Can't export the chat"),
		point(icRobot, "Can't use Meta AI features in this chat"),
		point(icDevices, "Can't use linked devices for this chat. This helps to prevent people in the chat from using "+
			"unofficial clients or AI agents to access it."),
		u.infoSection("Our privacy promise", false),
		para("Whether or not you turn on these restrictions, the messages and calls in this chat are protected with "+
			"end-to-end encryption.", p.TextSecondary, 14, 0, 24),
	}
}

// infoPara is a paragraph of a page, as wide as it's given.
func (u *UI) infoPara(txt string, col color.NRGBA, size unit.Sp) layout.Widget {
	return func(gtx C) D {
		l := u.label(size, txt, col)
		l.MaxLines = 0
		l.LineHeight, l.LineHeightScale = unit.Sp(float32(size)*1.45), 1
		d := l.Layout(gtx)
		d.Size.X = gtx.Constraints.Max.X
		return d
	}
}

// changeRows lists who joined, left or changed role.
func (u *UI) changeRows() []layout.Widget {
	p := u.pal
	if len(u.info.changes) == 0 {
		return []layout.Widget{func(gtx C) D {
			return layout.Inset{Left: infoPadX, Right: 24, Top: 40}.Layout(gtx, func(gtx C) D {
				l := u.label(15, "No member changes yet. Changes show here from when this computer sees them.", p.TextSecondary)
				l.MaxLines = 0
				return l.Layout(gtx)
			})
		}}
	}
	var rows []layout.Widget
	for _, ch := range u.info.changes {
		rows = append(rows, func(gtx C) D {
			return layout.Inset{Left: infoPadX, Right: 24, Top: 10, Bottom: 10}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(u.label(15.5, memberChangeText(ch), p.Text, labelOpts{maxLines: 2}).Layout),
					layout.Rigid(layout.Spacer{Height: 3}.Layout),
					layout.Rigid(u.label(13, dateChip(ch.Time, u.now())+" at "+ch.Time.Format("15:04"), p.TextSecondary).Layout),
				)
			})
		})
	}
	return rows
}

// memberChangeText says what a MemberChange did, like WhatsApp's notices.
func memberChangeText(c model.MemberChange) string {
	name, by := plainText(c.Name), plainText(c.By)
	switch c.Action {
	case model.MemberJoined:
		return name + " joined"
	case model.MemberLeft:
		return name + " left"
	case model.MemberAdded:
		return by + " added " + name
	case model.MemberRemoved:
		return by + " removed " + name
	case model.MemberPromoted:
		if by == "" {
			return name + " is now an admin"
		}
		return by + " made " + name + " an admin"
	case model.MemberDemoted:
		if by == "" {
			return name + " is no longer an admin"
		}
		return by + " dismissed " + name + " as admin"
	}
	return name
}

// exportChat exports a chat, unless Restricted chat keeps it in.
func (u *UI) exportChat(id string) {
	if info := u.timerInfo(id); info != nil && info.LimitSharing {
		u.inform("Can't export chat", "Restricted chat is on in this chat, so nobody can export it.")
		return
	}
	u.backend.ExportChat(id)
}

func onOff(on bool) string {
	if on {
		return "On"
	}
	return "Off"
}

// infoItem is a row of the info panel's settings.
func (u *UI) infoItem(key string, it listItem) layout.Widget {
	return func(gtx C) D { return u.layoutListItem(gtx, u.btn("info:"+key), it, infoGeom) }
}

// infoChatActions runs the info panel's settings rows clicked last frame.
func (u *UI) infoChatActions(gtx C, c *model.Chat, info *model.ChatInfo) {
	clicked := func(key string) bool { return u.btn("info:" + key).Clicked(gtx) }
	name := info.Name
	if name == "" {
		name = c.Name
	}
	chat := u.chatByID(c.ID)
	if chat == nil {
		chat = c
	}
	if clicked("starred") {
		u.openInfoSub(infoStarred)
	}
	if clicked("perms") {
		u.openInfoSub(infoPerms)
	}
	if clicked("mentions") {
		u.openInfoSub(infoMentions)
	}
	if clicked("transcripts") {
		u.openInfoSub(infoTranscripts)
	}
	if clicked("theme") {
		u.openChatTheme(chat)
	}
	if clicked("encryption") {
		u.openEncryption(chat, name)
	}
	if clicked("disappearing") {
		u.openTimerMenu(chat)
	}
	if clicked("privacy") {
		u.openInfoSub(infoRestricted)
	}
	if clicked("community") {
		u.inform("Add group to a community", "This app can't add groups to communities yet. "+
			"Do it from WhatsApp on your phone.")
	}
	if clicked("add") {
		u.openAddMembers(chat)
	}
	if clicked("link") {
		u.openInviteLink(c.ID)
	}
	if clicked("email") {
		u.inviteByEmail(c.ID, name)
	}
}
