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

	unread := 0
	for _, c := range u.chats {
		if c.Unread > 0 && !c.Archived {
			unread++
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
	chats := glyph(icChats)
	if !u.sidebar.showArchived {
		chats = func(gtx C, col color.NRGBA) D { return chatsIcon(gtx, 24, col, p.RailActive) }
	}
	archive := glyph(icArchive)
	if u.sidebar.showArchived {
		archive = glyph(icArchiveOn)
	}

	gtx.Constraints = layout.Exact(sz)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(layout.Spacer{Height: 10}.Layout),
		item(&u.rail.chats, !u.sidebar.showArchived, chats, unread, false),
		item(&u.rail.calls, false, glyph(icCall), 0, false),
		item(&u.rail.status, false, func(gtx C, col color.NRGBA) D { return statusIcon(gtx, 24, col) }, 0, false),
		item(&u.rail.channels, false, func(gtx C, col color.NRGBA) D { return channelsIcon(gtx, 24, col) }, 0, true),
		item(&u.rail.communities, false, glyph(icGroupsFill), 0, false),
		layout.Rigid(func(gtx C) D {
			w := gtx.Dp(42)
			x := (gtx.Constraints.Max.X - w) / 2
			y := gtx.Dp(8)
			fillRect(gtx, image.Rect(x, y, x+w, y+max(1, gtx.Dp(1))), p.RailSeparator)
			return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(21))}
		}),
		item(&u.rail.archived, u.sidebar.showArchived, archive, 0, false),
		layout.Flexed(1, layout.Spacer{}.Layout),
		item(&u.rail.media, false, glyph(icMedia), 0, false),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 2, Bottom: 17}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.N.Layout(gtx, func(gtx C) D {
					return clickable(gtx, &u.rail.profile, func(gtx C) D {
						return centerIn(gtx, gtx.Dp(42), func(gtx C) D {
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
	return clickable(gtx, c, func(gtx C) D {
		sz := gtx.Dp(42)
		switch {
		case active:
			fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, p.RailActive)
		case c.Hovered():
			fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, p.Hover)
		}
		col := p.Icon
		if active {
			col = p.IconActive
		}
		off := (sz - gtx.Dp(24)) / 2
		t := op.Offset(image.Pt(off, off)).Push(gtx.Ops)
		glyph(gtx, col)
		t.Pop()

		switch {
		case badge > 0:
			m := op.Record(gtx.Ops)
			bd := u.railBadge(gtx, badge)
			call := m.Stop()
			t := op.Offset(image.Pt(sz-bd.Size.X+gtx.Dp(3), -gtx.Dp(1))).Push(gtx.Ops)
			call.Add(gtx.Ops)
			t.Pop()
		case dot:
			fillCircle(gtx, image.Pt(sz-gtx.Dp(10), gtx.Dp(10)), gtx.Dp(5), p.Green)
		}
		return D{Size: image.Pt(sz, sz)}
	})
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
