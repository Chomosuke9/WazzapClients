package ui

import (
	"image"

	"gioui.org/font"
	"gioui.org/layout"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// Pages that open inside the info panel, with a back arrow: a chat's
// starred messages, a group's permissions and its member changes.

const (
	infoStarred = "starred"
	infoPerms   = "perms"
	infoChanges = "changes"
)

// openInfoSub shows one of the panel's pages.
func (u *UI) openInfoSub(sub string) {
	u.info.sub = sub
	u.info.subPos = u.info.list.Position
	u.info.list.Position = layout.Position{}
	switch sub {
	case infoStarred:
		u.info.starred.start(u.backend, model.GalleryQuery{Kind: model.GalleryStarred, ChatID: u.info.chatID, Limit: 50})
	case infoChanges:
		u.info.changes = u.backend.MemberChanges(u.info.chatID)
	}
}

// closeInfoSub returns to the panel's main page.
func (u *UI) closeInfoSub() {
	u.info.sub = ""
	u.info.list.Position = u.info.subPos
}

func infoSubTitle(sub string) string {
	switch sub {
	case infoStarred:
		return "Starred messages"
	case infoPerms:
		return "Group permissions"
	case infoChanges:
		return "Member changes"
	}
	return ""
}

// infoSubRows builds a page's rows.
func (u *UI) infoSubRows(gtx C, c *model.Chat, info *model.ChatInfo) []layout.Widget {
	switch u.info.sub {
	case infoStarred:
		return u.starredRows(gtx)
	case infoPerms:
		return u.permRows(gtx, c, info)
	case infoChanges:
		return u.changeRows()
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
	if !u.gallery.list.loaded(e) {
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
		rows = append(rows, func(gtx C) D { return u.starredRow(gtx, m) })
	}
	return rows
}

func (u *UI) starredRow(gtx C, m *model.Message) D {
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
	txt := plainText(m.Text)
	if m.Media != model.MediaNone {
		txt = mediaLabel(m)
	}
	return clickable(gtx, c, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		bg := mix(p.Panel, p.Hover, u.hover(gtx, c))
		return background(gtx, bg, 0, func(gtx C) D {
			return layout.Inset{Left: infoPadX, Right: 24, Top: 12, Bottom: 12}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return u.avatar(gtx, m.SenderID, who, false, 24) }),
							layout.Rigid(layout.Spacer{Width: 10}.Layout),
							layout.Flexed(1, u.label(14.5, plainText(who), p.Text, labelOpts{weight: font.Medium, maxLines: 1}).Layout),
							layout.Rigid(u.label(13, listTime(m.Time, u.now()), p.TextSecondary, labelOpts{maxLines: 1}).Layout),
						)
					}),
					layout.Rigid(layout.Spacer{Height: 8}.Layout),
					layout.Rigid(func(gtx C) D {
						bub := p.BubbleIn
						if m.FromMe {
							bub = p.BubbleOut
						}
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
	return []layout.Widget{
		u.infoSection("Members can:", false),
		u.permRow("edit", icEdit, "Edit group settings",
			"This includes the name, icon, description, disappearing message timer, and the ability to pin messages.",
			!info.Locked, set(model.GroupLock, !info.Locked)),
		u.permRow("send", icChats, "Send new messages", "", !info.Announce, set(model.GroupAnnounce, !info.Announce)),
		u.permRow("addmembers", icPersonAdd, "Add other members", "", !info.AdminsAdd, set(model.GroupAddMode, !info.AdminsAdd)),
		func(gtx C) D { return layout.Spacer{Height: 12}.Layout(gtx) },
		u.infoSection("Admins can:", true),
		u.permRow("approve", icPersonAddFill, "Approve new members",
			"When turned on, admins must approve anyone who wants to join this group.",
			info.Approval, set(model.GroupApproval, !info.Approval)),
	}
}

// permRow is a permission with its checkbox.
func (u *UI) permRow(key string, ic *icon.Icon, title, sub string, on bool, run func()) layout.Widget {
	return func(gtx C) D {
		p := u.pal
		b := u.btn("perm:" + key)
		if b.Clicked(gtx) {
			run()
		}
		box, col := icCheckBoxEmpty, p.TextSecondary
		if on {
			box, col = icCheckBox, p.Green
		}
		g := infoGeom
		g.subHeight = 72
		return u.layoutListItem(gtx, b, listItem{ic: ic, title: title, sub: sub,
			trailing: func(gtx C) D { return layout.Inset{Left: 12, Right: 8}.Layout(gtx, iconW(box, 24, col)) }}, g)
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
		u.inform("Advanced chat privacy", "This app can't change advanced chat privacy yet. "+
			"Turn it on or off from WhatsApp on your phone.")
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
