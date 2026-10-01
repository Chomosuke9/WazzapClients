package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// infoState is the contact / group info panel beside a conversation.
type infoState struct {
	open bool
	// from is the open chat the panel belongs to; chatID is whose details
	// it shows: the chat itself, or a group member.
	from, chatID string
	name         string // shown until the details load
	back         []infoPage
	data         *model.ChatInfo
	list         widget.List
	closeBtn     widget.Clickable
	allMembers   bool  // the member list is expanded
	allHours     bool  // a business's opening hours are expanded
	anim         tween // sliding in and out
	// memberSearch filters a group's member list by memberQuery.
	memberSearch bool
	memberQuery  widget.Editor
}

// infoPage is a panel to return to with the back arrow (a group's info
// under a member's).
type infoPage struct {
	chatID, name string
	pos          layout.Position
}

// shown reports whether the panel is open or still sliding away.
func (s *infoState) shown() bool { return s.open || s.anim.v > 0 }

// hideInfo closes the panel at once, for when the chat or page changes
// under it.
func (u *UI) hideInfo() {
	u.info.open = false
	u.info.back = nil
	u.info.anim.snap(false)
}

// infoMembersShown is how many members a group's panel lists before
// "View all", like WhatsApp.
const infoMembersShown = 10

func (u *UI) openInfo(chatID string) {
	if !u.info.open || u.info.chatID != chatID {
		u.info.back = nil
	}
	u.info.open = true
	if u.selected != nil {
		u.info.from = u.selected.ID
	}
	u.showInfo(chatID, "")
}

// openContact shows a person's contact info, from a sender's name in a
// group or the group's member list. The back arrow returns to the group's
// info when that was open.
func (u *UI) openContact(id, name string) {
	if id == "" || u.selected == nil {
		return
	}
	switch {
	case !u.info.open:
		u.info.back = nil
	case u.info.chatID != id:
		u.info.back = append(u.info.back, infoPage{u.info.chatID, u.info.name, u.info.list.Position})
	}
	u.info.open = true
	u.info.from = u.selected.ID
	u.showInfo(id, strings.TrimPrefix(plainText(name), "~"))
}

// infoBack returns to the previous panel.
func (u *UI) infoBack() {
	n := len(u.info.back)
	pg := u.info.back[n-1]
	u.info.back = u.info.back[:n-1]
	u.showInfo(pg.chatID, pg.name)
	u.info.list.Position = pg.pos
}

func (u *UI) showInfo(chatID, name string) {
	u.swapSearchForInfo()
	if u.info.chatID != chatID {
		u.info.list.Position = layout.Position{}
		u.info.allMembers = false
		u.info.allHours = false
		u.info.memberSearch = false
	}
	u.info.chatID = chatID
	u.info.name = name
	u.info.data = u.backend.Info(chatID)
}

// Geometry of the panel's list rows, in dp from the panel's left edge.
var infoGeom = listGeom{hoverLeft: 11, hoverRight: 18, iconCenter: 41.2, textLeft: 77.4, height: 62,
	padY: 9.4, padBottom: 7, padRight: 30, softDanger: true}

const infoPadX = 21 // left edge of dividers, the description and the footer

func (u *UI) layoutInfo(gtx C) D {
	p := u.pal
	if u.info.closeBtn.Clicked(gtx) {
		if len(u.info.back) > 0 {
			u.infoBack()
		} else {
			u.info.open = false
		}
	}
	c := u.chatByID(u.info.chatID)
	if c == nil {
		// A group member you have no chat with.
		c = &model.Chat{ID: u.info.chatID, Name: u.info.name}
	}
	info := u.info.data
	if info == nil {
		info = &model.ChatInfo{ID: c.ID, Name: c.Name, IsGroup: c.IsGroup}
	}
	if info.Name == "" || (c.Name != "" && !info.IsGroup) {
		info.Name = c.Name
	}
	if info.Name == "" {
		info.Name = info.Phone
	}
	dims := fill(gtx, p.Panel)
	rows := u.infoRows(gtx, c, info)
	closeIc := icClose
	if len(u.info.back) > 0 {
		closeIc = icBack
	}
	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			title := "Contact info"
			if c.IsGroup {
				title = "Group info"
			}
			return vcenter(gtx, gtx.Dp(64), func(gtx C) D {
				return layout.Inset{Left: 11.5}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &u.info.closeBtn, closeIc, 40, 25, p.IconStrong) }),
						layout.Rigid(layout.Spacer{Width: 10}.Layout),
						layout.Flexed(1, u.label(16.5, title, p.Text, labelOpts{maxLines: 1}).Layout),
					)
				})
			})
		}),
		layout.Flexed(1, func(gtx C) D {
			return u.scrollList(gtx, &u.info.list, len(rows), func(gtx C, i int) D { return rows[i](gtx) })
		}),
	)
	return dims
}

// infoRows builds the panel's scrolling content.
func (u *UI) infoRows(gtx C, c *model.Chat, info *model.ChatInfo) []layout.Widget {
	p := u.pal
	if !info.IsGroup {
		return u.contactRows(gtx, c, info)
	}
	rows := []layout.Widget{
		func(gtx C) D { return u.infoProfile(gtx, c, info) },
	}
	if info.IsGroup || info.About != "" {
		rows = append(rows, func(gtx C) D { return u.infoAbout(gtx, info) })
	}
	rows = append(rows, u.infoDivider(0, 8),
		func(gtx C) D { return u.infoMedia(gtx, info) },
		u.infoDivider(12.2, 6),
	)
	item := func(key string, it listItem) layout.Widget {
		return func(gtx C) D { return u.layoutListItem(gtx, u.btn("info:"+key), it, infoGeom) }
	}
	// The actions at the bottom of the panel are spaced a little wider.
	tall := infoGeom
	tall.height = 65.5
	action := func(key string, it listItem) layout.Widget {
		return func(gtx C) D { return u.layoutListItem(gtx, u.btn("info:"+key), it, tall) }
	}
	notif := "All messages"
	notifIc := icBell
	if c.Muted {
		notif, notifIc = "Muted always", icMuted
	}
	disappearing := "Off"
	if d := info.Disappearing; d > 0 {
		disappearing = durationLabel(d)
	}
	rows = append(rows,
		item("starred", listItem{ic: icStar, title: "Starred messages"}),
		item("notif", listItem{ic: notifIc, title: "Notification settings", sub: notif}),
		item("theme", listItem{ic: icPalette, title: "Chat theme"}),
		item("encryption", listItem{ic: icLockOutline, title: "Encryption", sub: "Messages are end-to-end encrypted. Click to learn more."}),
		item("disappearing", listItem{glyph: disappearingIcon, title: "Disappearing messages", sub: disappearing}),
		item("privacy", listItem{ic: icShield, title: "Advanced chat privacy", sub: "Off"}),
	)
	rows = append(rows,
		item("perms", listItem{ic: icSettings, title: "Group permissions"}),
		u.infoDivider(12, 3.2),
		func(gtx C) D {
			return u.infoCircleRow(gtx, "community", func(gtx C) D {
				return roundedSquare(gtx, 52, 12, p.Green, icGroupsFill, 32, rgb(0xffffff))
			}, listItem{title: "Add group to a community", sub: "Bring members together in topic-based groups",
				trailing: func(gtx C) D { return layout.Inset{Right: 27}.Layout(gtx, iconW(icChevronRight, 24, p.TextSecondary)) }}, 107)
		},
		item("similar", listItem{ic: icGroupAdd, title: "Create a similar group", sub: "Start with the same members. You can add or remove people."}),
		func(gtx C) D { return u.infoMembersHeader(gtx, info) },
		u.greenAction("add", icPersonAddFill, "Add member", false),
		u.greenAction("link", icLink, "Invite to group via link", false),
		u.greenAction("email", icMail, "Invite to group via email", true),
	)
	members := info.Members
	if u.info.memberSearch {
		members = filterMembers(members, u.info.memberQuery.Text())
		if len(members) == 0 {
			rows = append(rows, func(gtx C) D {
				return layout.Inset{Left: infoPadX, Top: 14, Bottom: 14}.Layout(gtx, u.label(15, "No members found", p.TextSecondary).Layout)
			})
		}
	} else if !u.info.allMembers && len(members) > infoMembersShown {
		members = members[:infoMembersShown]
	}
	for _, m := range members {
		rows = append(rows, func(gtx C) D { return u.infoMember(gtx, m) })
	}
	if more := len(info.Members) - len(members); more > 0 {
		rows = append(rows, func(gtx C) D {
			c := u.btn("info:allmembers")
			if c.Clicked(gtx) {
				u.info.allMembers = true
			}
			return layout.Inset{Left: infoPadX + 66, Top: 10, Bottom: 10}.Layout(gtx, func(gtx C) D {
				return clickable(gtx, c, u.label(16.5, fmt.Sprintf("View all (%d more)", more), p.Green).Layout)
			})
		})
	}
	rows = append(rows,
		func(gtx C) D { return layout.Spacer{Height: 1}.Layout(gtx) },
		action("changes", listItem{ic: icList, title: "See member changes"}),
		action("fav", listItem{ic: icHeart, title: "Add to favourites"}),
		action("list", listItem{ic: icAddToList, title: "Add to list"}),
		action("clear", listItem{ic: icClear, title: "Clear chat", danger: true}),
		action("exit", listItem{ic: icLogout, title: "Exit group", danger: true}),
		action("report", listItem{ic: icThumbDown, title: "Report group", danger: true}),
	)
	if !info.Created.IsZero() {
		by := ""
		if info.CreatedBy != "" {
			by = " by " + info.CreatedBy
		}
		txt := "Group created" + by + ", on " + info.Created.Format("02/01/2006") + " at " + info.Created.Format("15:04")
		rows = append(rows, func(gtx C) D {
			return layout.Inset{Left: infoPadX, Right: 24, Top: 15.5, Bottom: 40}.Layout(gtx,
				u.label(15, txt, p.TextSecondary, labelOpts{maxLines: 2}).Layout)
		})
	}
	return rows
}

func durationLabel(secs uint32) string {
	switch {
	case secs >= 86400*90:
		return "90 days"
	case secs >= 86400*7:
		return "7 days"
	case secs >= 86400:
		return "24 hours"
	}
	return fmt.Sprintf("%d seconds", secs)
}

func (u *UI) infoDivider(top, bottom unit.Dp) layout.Widget {
	return func(gtx C) D {
		return layout.Inset{Left: infoPadX, Right: 30, Top: top, Bottom: bottom}.Layout(gtx, func(gtx C) D {
			h := max(1, gtx.Dp(1))
			fillRect(gtx, image.Rect(0, 0, gtx.Constraints.Max.X, h), u.pal.Hover)
			return D{Size: image.Pt(gtx.Constraints.Max.X, h)}
		})
	}
}

// infoProfile is the picture, name, subtitle and quick actions.
func (u *UI) infoProfile(gtx C, c *model.Chat, info *model.ChatInfo) D {
	p := u.pal
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	contentW := gtx.Constraints.Max.X - gtx.Dp(8) // scrollbar gutter
	center := func(w layout.Widget) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = contentW
			gtx.Constraints.Max.X = contentW
			return layout.N.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = 0
				return w(gtx)
			})
		})
	}
	var sub layout.Widget
	if info.IsGroup {
		n := len(info.Members)
		members := fmt.Sprintf("%d members", n)
		if n == 1 {
			members = "1 member"
		}
		sub = func(gtx C) D {
			return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
				layout.Rigid(u.label(16.8, "Group · ", p.TextSecondary).Layout),
				layout.Rigid(u.label(16.8, members, p.Green, labelOpts{weight: font.SemiBold}).Layout),
			)
		}
		if n == 0 {
			sub = u.label(16.8, "Group", p.TextSecondary).Layout
		}
	} else {
		sub = u.contactSubtitle(c, info)
	}
	type action struct {
		key   string
		ic    *icon.Icon
		label string
	}
	var actions []action
	switch {
	case !info.IsGroup && (u.selected == nil || u.selected.ID != c.ID):
		// A group member's info offers to message them instead.
		actions = []action{{"message", icChats, "Message"}, {"share", icForward, "Share"}}
	case info.IsGroup:
		actions = []action{{"voice", icCallLine, "Voice"}, {"video", icVideoLine, "Video"}, {"add", icPersonAdd, "Add"}, {"search", icSearch, "Search"}}
	default:
		actions = []action{{"voice", icCallLine, "Voice"}, {"video", icVideoLine, "Video"}, {"search", icSearch, "Search"}}
	}
	if u.btn("info-action:message").Clicked(gtx) {
		u.openDirect(c.ID, info.Name)
	}
	if u.btn("info-action:share").Clicked(gtx) {
		u.openShareContact(c.ID)
	}
	if u.btn("info-action:search").Clicked(gtx) {
		u.openChatSearch()
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(layout.Spacer{Height: 26}.Layout),
		center(func(gtx C) D {
			px := gtx.Dp(127)
			if info.IsGroup {
				if e := u.avatarImage(c.ID); e != nil {
					r := image.Rect(0, 0, px, px)
					defer clip.Ellipse(r).Push(gtx.Ops).Pop()
					paintCover(gtx, e.op, e.size, r)
					return D{Size: r.Size()}
				}
				// A group without a photo offers to add one.
				fillCircle(gtx, image.Pt(px/2, px/2), px/2, p.GroupAvatar)
				return centerIn(gtx, px, iconW(icAddPhoto, 56, p.GroupAvatarIcon))
			}
			return u.avatar(gtx, c.ID, c.Name, c.IsGroup, 127)
		}),
		layout.Rigid(layout.Spacer{Height: 19.5}.Layout),
		layout.Rigid(func(gtx C) D {
			// The name is centered; the edit pencil sits at the right edge.
			gtx.Constraints.Min.X = contentW
			gtx.Constraints.Max.X = contentW
			d := layout.N.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = 0
				gtx.Constraints.Max.X = max(0, contentW-2*gtx.Dp(64))
				if info.Business != nil {
					// A business's profile has a smaller, bold name.
					return u.label(20, info.Name, p.Text, labelOpts{weight: font.SemiBold, maxLines: 2, align: text.Middle}).Layout(gtx)
				}
				return u.label(25.5, info.Name, p.Text, labelOpts{maxLines: 2, align: text.Middle}).Layout(gtx)
			})
			if info.IsGroup {
				pc := gtx.Dp(26)
				part := record(gtx, iconW(icEdit, 26, p.IconStrong))
				part.at(gtx, contentW-gtx.Dp(40)-pc/2, (d.Size.Y-pc)/2)
			}
			return d
		}),
		layout.Rigid(layout.Spacer{Height: 7.3}.Layout),
		center(sub),
		layout.Rigid(func(gtx C) D {
			if info.Business != nil {
				return layout.Spacer{Height: 19}.Layout(gtx)
			}
			return layout.Spacer{Height: 15}.Layout(gtx)
		}),
		center(func(gtx C) D {
			var children []layout.FlexChild
			for i, a := range actions {
				if i > 0 {
					children = append(children, layout.Rigid(layout.Spacer{Width: 17}.Layout))
				}
				children = append(children, layout.Rigid(func(gtx C) D { return u.infoAction(gtx, a.key, a.ic, a.label) }))
			}
			return layout.Flex{}.Layout(gtx, children...)
		}),
		layout.Rigid(layout.Spacer{Height: 23.8}.Layout),
	)
}

// infoAction is one of the pill buttons under the name (Voice, Video, ...).
func (u *UI) infoAction(gtx C, key string, ic *icon.Icon, label string) D {
	p := u.pal
	c := u.btn("info-action:" + key)
	return clickable(gtx, c, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				sz := image.Pt(gtx.Dp(63), gtx.Dp(50.5))
				bg := mix(p.Hover, rgb(0xffffff), 0.06*u.hover(gtx, c))
				fillRRect(gtx, image.Rectangle{Max: sz}, sz.Y/2, bg)
				gtx.Constraints = layout.Exact(sz)
				return layout.Center.Layout(gtx, iconW(ic, 27, p.IconStrong))
			}),
			layout.Rigid(layout.Spacer{Height: 7}.Layout),
			layout.Rigid(u.label(14.5, label, p.Text, labelOpts{maxLines: 1}).Layout),
		)
	})
}

// infoAbout shows a group's description or a contact's about text.
func (u *UI) infoAbout(gtx C, info *model.ChatInfo) D {
	p := u.pal
	txt := info.About
	col := p.Text
	if txt == "" {
		txt, col = "Add group description", p.Green
	}
	return layout.Inset{Left: infoPadX, Right: 30, Top: 6, Bottom: 31}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D {
				l := u.label(17, txt, col, labelOpts{})
				l.MaxLines = 6
				l.LineHeight, l.LineHeightScale = 25, 1
				return l.Layout(gtx)
			}),
			layout.Rigid(func(gtx C) D {
				if !info.IsGroup {
					return D{}
				}
				return layout.Inset{Left: 16, Right: 1}.Layout(gtx, iconW(icEdit, 26, p.IconStrong))
			}),
		)
	})
}

// infoMedia is the "Media, links and docs" row with the newest pictures.
func (u *UI) infoMedia(gtx C, info *model.ChatInfo) D {
	p := u.pal
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return u.layoutListItem(gtx, u.btn("info:media"), listItem{ic: icPermMedia, title: "Media, links and docs",
				trailing: func(gtx C) D {
					if info.MediaCount == 0 {
						return D{}
					}
					return layout.Inset{Right: 22.5}.Layout(gtx, u.label(16.8, itoa(info.MediaCount), p.TextSecondary).Layout)
				}}, infoGeom)
		}),
		layout.Rigid(func(gtx C) D {
			if len(info.Media) == 0 {
				return D{}
			}
			return layout.Inset{Left: 29.5, Right: 37, Top: 10.5, Bottom: 19.7}.Layout(gtx, func(gtx C) D {
				gap := gtx.Dp(8.3)
				w := (gtx.Constraints.Max.X - 3*gap) / 4
				h := gtx.Dp(90.3)
				for i, m := range info.Media {
					if i == 4 {
						break
					}
					r := image.Rect(i*(w+gap), 0, i*(w+gap)+w, h)
					u.layoutImage(gtx, r, m, u.messageImage(m, 2*w))
				}
				return D{Size: image.Pt(gtx.Constraints.Max.X, h)}
			})
		}),
	)
}

// infoCircleRow is a row led by a 52dp picture instead of an icon.
func (u *UI) infoCircleRow(gtx C, key string, pic layout.Widget, it listItem, height unit.Dp) D {
	g := infoGeom
	if it.trailing != nil && it.sub != "" {
		g.padRight = 40
	}
	g.iconCenter = infoPadX - g.hoverLeft + 26
	g.textLeft = 76
	g.height, g.subHeight = height, height
	it.glyph = func(gtx C, col color.NRGBA) D { return pic(gtx) }
	return u.layoutListItem(gtx, u.btn("info:"+key), it, g)
}

// greenAction is a member-list action with a green round icon.
func (u *UI) greenAction(key string, ic *icon.Icon, title string, isNew bool) layout.Widget {
	return func(gtx C) D {
		p := u.pal
		it := listItem{title: title}
		if isNew {
			it.trailing = func(gtx C) D { return layout.Inset{Right: 12}.Layout(gtx, u.pill("New", 13.5, true)) }
		}
		return u.infoCircleRow(gtx, key, func(gtx C) D {
			px := gtx.Dp(52)
			fillCircle(gtx, image.Pt(px/2, px/2), px/2, p.Green)
			return centerIn(gtx, px, iconW(ic, 27, p.OnGreen))
		}, it, 77)
	}
}

// pill is a small green-tinted label ("Group admin", "New").
func (u *UI) pill(txt string, size unit.Sp, round bool) layout.Widget {
	return func(gtx C) D {
		p := u.pal
		m := record(gtx, func(gtx C) D {
			return layout.Inset{Left: 9, Right: 9, Top: 2.5, Bottom: 3}.Layout(gtx,
				u.label(size, txt, p.ChipActiveText, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout)
		})
		r := gtx.Dp(6)
		if round {
			r = m.size.Y / 2
		}
		fillRRect(gtx, image.Rectangle{Max: m.size}, r, p.ChipActive)
		m.at(gtx, 0, 0)
		return D{Size: m.size}
	}
}

func (u *UI) infoMembersHeader(gtx C, info *model.ChatInfo) D {
	n := len(info.Members)
	txt := fmt.Sprintf("%d members", n)
	if n == 1 {
		txt = "1 member"
	}
	// The search button's 40dp box ends 6dp right of its icon.
	return layout.Inset{Left: infoPadX, Right: 34, Top: 32.2 - 6, Bottom: 13.7 - 6}.Layout(gtx, func(gtx C) D {
		return u.membersHeaderRow(gtx, txt)
	})
}

func (u *UI) infoMember(gtx C, m model.Member) D {
	it := listItem{title: m.Name}
	if m.Me {
		it.sub = "Add member tag"
	}
	if m.Admin {
		it.trailing = func(gtx C) D { return layout.Inset{Right: 17}.Layout(gtx, u.pill("Group admin", 12.3, false)) }
	}
	g := infoGeom
	g.iconCenter = infoPadX - g.hoverLeft + 25.5
	g.textLeft = 75.4
	g.height, g.subHeight = 76, 76
	it.glyph = func(gtx C, col color.NRGBA) D { return u.memberAvatar(gtx, m) }
	key := "member:" + m.ID
	c := u.btn(key)
	if c.Clicked(gtx) && !m.Me {
		u.openContact(m.ID, m.Name)
	}
	return u.layoutMemberItem(gtx, c, it, g, m.Me)
}

// memberAvatar draws a member's picture, ringed when they have a status.
func (u *UI) memberAvatar(gtx C, m model.Member) D {
	id, name := m.ID, m.Name
	if m.Me {
		id, name = u.meID, u.meName()
	}
	for _, t := range u.statuses {
		if (t.Mine && m.Me) || (!t.Mine && t.ID == m.ID) {
			return u.ringedAvatar(gtx, t, id, name, 51)
		}
	}
	return u.avatar(gtx, id, name, false, 51)
}

// ringedAvatar is a profile picture inside a status ring.
func (u *UI) ringedAvatar(gtx C, t *model.StatusThread, id, name string, size unit.Dp) D {
	p := u.pal
	px := gtx.Dp(size)
	col := p.Green
	if t.Viewed() && !t.Mine {
		col = p.RingViewed
	}
	fillCircle(gtx, image.Pt(px/2, px/2), px/2, col)
	ring := gtx.Dp(2)
	fillCircle(gtx, image.Pt(px/2, px/2), px/2-ring, p.Panel)
	inset := gtx.Dp(4)
	tr := op.Offset(image.Pt(inset, inset)).Push(gtx.Ops)
	u.avatar(gtx, id, name, false, dp(gtx, px-2*inset))
	tr.Pop()
	return D{Size: image.Pt(px, px)}
}

// layoutMemberItem is layoutListItem with a green subtitle for your own row.
func (u *UI) layoutMemberItem(gtx C, c *widget.Clickable, it listItem, g listGeom, me bool) D {
	if !me {
		return u.layoutListItem(gtx, c, it, g)
	}
	p := u.pal
	title, sub := it.title, it.sub
	it.content = func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(u.label(17, title, p.Text).Layout),
			layout.Rigid(layout.Spacer{Height: 2}.Layout),
			layout.Rigid(u.label(14.5, sub, p.Green, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
		)
	}
	return u.layoutListItem(gtx, c, it, g)
}

// disappearingIcon is WhatsApp's disappearing-messages glyph: a timer dial,
// solid on the left and dotted on the right, with a hand.
func disappearingIcon(gtx C, col color.NRGBA) D {
	return cachedGlyph(gtx, glyphKey{name: "disappearing", px: gtx.Dp(26), col: col},
		func(gtx C) D { return drawDisappearingIcon(gtx, col) })
}

func drawDisappearingIcon(gtx C, col color.NRGBA) D {
	px := float32(gtx.Dp(26))
	u := px / 26
	c := f32.Pt(13*u, 13*u)
	r := 8.5 * u
	strokeArc(gtx, c, r, math.Pi/2, math.Pi, 2*u, col)
	for _, deg := range []float64{-60, -30, 0, 30, 60} {
		a := deg * math.Pi / 180
		p := c.Add(f32.Pt(r*float32(math.Cos(a)), r*float32(math.Sin(a))))
		fillCircle(gtx, image.Pt(int(p.X), int(p.Y)), int(1.25*u+0.5), col)
	}
	// The hand points up and to the right.
	dir := f32.Pt(float32(math.Cos(-math.Pi/4)), float32(math.Sin(-math.Pi/4)))
	var hand clip.Path
	hand.Begin(gtx.Ops)
	hand.MoveTo(c.Add(dir.Mul(0.3 * u)))
	hand.LineTo(c.Add(dir.Mul(5.2 * u)))
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: hand.End(), Width: 2.3 * u}.Op())
	return D{Size: image.Pt(int(px), int(px))}
}
