package ui

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

const railWidth = unit.Dp(68)

// layoutRail draws the navigation column on the far left. It sits on the
// window frame color; the panels to its right have their own background.
func (u *UI) layoutRail(gtx C) D {
	p := u.pal
	sz := gtx.Constraints.Max

	unread, archivedUnread := 0, 0
	for _, c := range u.chats {
		switch {
		case c.Unread == 0:
		case !c.Archived:
			unread++
		case !c.Muted:
			archivedUnread++
		}
	}
	glyph := func(ic *icon.Icon) func(gtx C, col color.NRGBA) D {
		return func(gtx C, col color.NRGBA) D { return drawIcon(gtx, ic, 24, col) }
	}
	item := func(c *widget.Clickable, active bool, g func(gtx C, col color.NRGBA) D, badge int, dot bool) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Bottom: 5}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.N.Layout(gtx, func(gtx C) D {
					return u.railButton(gtx, c, active, g, badge, dot)
				})
			})
		})
	}
	onChats := u.page == pageChats && !u.sidebar.showArchived
	onArchive := u.page == pageChats && u.sidebar.showArchived
	chats := func(gtx C, col color.NRGBA) D { return chatsOutline(gtx, 24, col) }
	if onChats {
		chats = func(gtx C, col color.NRGBA) D { return chatsIcon(gtx, 24, col, p.RailActive) }
	}
	archive := glyph(icArchive)
	if onArchive {
		archive = glyph(icArchiveOn)
	}
	status := func(gtx C, col color.NRGBA) D { return statusIcon(gtx, 24, col, u.page == pageStatus) }
	channels := func(gtx C, col color.NRGBA) D {
		return channelsIcon(gtx, 24, col, p.RailActive, u.page == pageChannels)
	}
	communities := func(gtx C, col color.NRGBA) D { return drawIcon(gtx, icGroupsLine, 27, col) }
	if u.page == pageCommunities {
		communities = func(gtx C, col color.NRGBA) D { return drawIcon(gtx, icGroupsFill, 27, col) }
	}
	// Dots mark news since the page was last opened.
	statusUnseen := false
	for _, t := range u.statuses {
		if !t.Mine && !t.Viewed() && t.Last().Time.After(u.statusSeen) {
			statusUnseen = true
		}
	}
	channelUnread := false
	for _, c := range u.channels {
		if c.Unread != 0 && c.Time.After(u.channelsSeen) {
			channelUnread = true
		}
	}
	// The active item's circle moves from one item to the next.
	var active *widget.Clickable
	switch {
	case onChats:
		active = &u.rail.chats
	case onArchive:
		active = &u.rail.archived
	case u.page == pageCalls:
		active = &u.rail.calls
	case u.page == pageStatus:
		active = &u.rail.status
	case u.page == pageChannels:
		active = &u.rail.channels
	case u.page == pageCommunities:
		active = &u.rail.communities
	case u.page == pageSettings:
		active = &u.rail.profile
	}
	u.railSel.step(gtx, active, durSwitch)

	gtx.Constraints = layout.Exact(sz)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(layout.Spacer{Height: 10}.Layout),
		item(&u.rail.chats, onChats, chats, unread, false),
		item(&u.rail.calls, u.page == pageCalls, glyph(icCall), 0, false),
		item(&u.rail.status, u.page == pageStatus, status, 0, statusUnseen),
		item(&u.rail.channels, u.page == pageChannels, channels, 0, channelUnread),
		item(&u.rail.communities, u.page == pageCommunities, communities, 0, false),
		layout.Rigid(func(gtx C) D {
			w := gtx.Dp(42)
			x := (gtx.Constraints.Max.X - w) / 2
			y := gtx.Dp(8)
			fillRect(gtx, image.Rect(x, y, x+w, y+max(1, gtx.Dp(1))), p.RailSeparator)
			return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(21))}
		}),
		item(&u.rail.archived, onArchive, archive, archivedUnread, false),
		layout.Flexed(1, layout.Spacer{}.Layout),
		item(&u.rail.media, u.gallery.open && u.gallery.chatID == "", glyph(icMedia), 0, false),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 2, Bottom: 17}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.N.Layout(gtx, func(gtx C) D {
					return clickable(gtx, &u.rail.profile, func(gtx C) D {
						sz := gtx.Dp(42)
						u.railCircle(gtx, &u.rail.profile, sz)
						return centerIn(gtx, sz, func(gtx C) D {
							return u.avatar(gtx, u.meID, u.meName(), false, 30)
						})
					})
				})
			})
		}),
	)
}

func (u *UI) railButton(gtx C, c *widget.Clickable, active bool, glyph func(gtx C, col color.NRGBA) D, badge int, dot bool) D {
	p := u.pal
	sz := gtx.Dp(42)
	d := clickable(gtx, c, func(gtx C) D {
		u.railCircle(gtx, c, sz)
		col := p.Icon
		if active {
			col = p.IconActive
		}
		g := record(gtx, func(gtx C) D { return glyph(gtx, col) })
		g.at(gtx, (sz-g.size.X)/2, (sz-g.size.Y)/2)
		if dot && badge <= 0 {
			fillCircle(gtx, image.Pt(sz-gtx.Dp(10), gtx.Dp(10)), gtx.Dp(5), p.Green)
		}
		return D{Size: image.Pt(sz, sz)}
	})
	// The badge sticks out past the button's corner, and a Clickable clips
	// what it draws to its size: draw it afterwards, on top.
	if badge > 0 {
		m := op.Record(gtx.Ops)
		bd := u.railBadge(gtx, badge)
		call := m.Stop()
		t := op.Offset(image.Pt(sz-bd.Size.X+gtx.Dp(3), -gtx.Dp(1))).Push(gtx.Ops)
		call.Add(gtx.Ops)
		t.Pop()
	}
	return d
}

// railCircle paints a rail button's round background: the active item's,
// fading between items, or the hover highlight.
func (u *UI) railCircle(gtx C, c *widget.Clickable, sz int) {
	p := u.pal
	a := u.railSel.of(c)
	if h := u.hover(gtx, c) * (1 - a); h > 0 {
		fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, faded(p.Hover, h))
	}
	if a > 0 {
		fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, faded(p.RailActive, a))
	}
}

// railBadge is the compact count bubble shown on rail icons.
func (u *UI) railBadge(gtx C, n int) D {
	h := gtx.Dp(18)
	gtx.Constraints.Min = image.Point{}
	m := op.Record(gtx.Ops)
	ld := u.label(unit.Sp(11), itoa(n), u.pal.OnGreen, labelOpts{weight: font.Bold, maxLines: 1}).Layout(gtx)
	call := m.Stop()
	w := max(h, ld.Size.X+gtx.Dp(10))
	ring := gtx.Dp(2)
	fillRRect(gtx, image.Rect(-ring, -ring, w+ring, h+ring), h/2+ring, u.pal.Frame)
	fillRRect(gtx, image.Rect(0, 0, w, h), h/2, u.pal.Green)
	t := op.Offset(image.Pt((w-ld.Size.X)/2, (h-ld.Size.Y)/2)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	return D{Size: image.Pt(w, h)}
}

func (u *UI) meName() string {
	if u.me == "" {
		return "Me"
	}
	return u.me
}
