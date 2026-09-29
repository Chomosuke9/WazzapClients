package ui

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
)

// layoutRail draws the narrow navigation column on the far left.
func (u *UI) layoutRail(gtx C) D {
	p := u.pal
	sz := gtx.Constraints.Max
	fillRect(gtx, image.Rectangle{Max: sz}, p.Rail)
	fillRect(gtx, image.Rect(sz.X-1, 0, sz.X, sz.Y), p.RailBorder)

	unreadChats := 0
	for _, c := range u.chats {
		if c.Unread > 0 {
			unreadChats++
		}
	}

	item := func(c *widget.Clickable, active bool, glyph func(gtx C, col color.NRGBA) D, badge int, dot bool) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.N.Layout(gtx, func(gtx C) D {
					return u.railButton(gtx, c, active, glyph, badge, dot)
				})
			})
		})
	}
	mat := func(data []byte) func(gtx C, col color.NRGBA) D {
		return func(gtx C, col color.NRGBA) D { return u.icons.layout(gtx, data, 24, col) }
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(layout.Spacer{Height: 10}.Layout),
		item(&u.rail.chats, true, mat(icChats), unreadChats, false),
		item(&u.rail.status, false, func(gtx C, col color.NRGBA) D { return statusIcon(gtx, 24, col) }, 0, true),
		item(&u.rail.channels, false, func(gtx C, col color.NRGBA) D { return channelsIcon(gtx, 24, col) }, 0, false),
		item(&u.rail.communities, false, mat(icGroup), 0, false),
		layout.Flexed(1, layout.Spacer{}.Layout),
		item(&u.rail.starred, false, mat(icStarred), 0, false),
		layout.Rigid(func(gtx C) D {
			// Separator between media shortcuts and settings.
			w := gtx.Dp(32)
			x := (gtx.Constraints.Max.X - w) / 2
			fillRect(gtx, image.Rect(x, gtx.Dp(6), x+w, gtx.Dp(6)+1), p.RailBorder)
			return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(13))}
		}),
		item(&u.rail.settings, false, mat(icSettings), 0, false),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 6, Bottom: 14}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.N.Layout(gtx, func(gtx C) D {
					return clickable(gtx, &u.rail.profile, func(gtx C) D {
						return u.avatar(gtx, "Me Myself", false, 32)
					})
				})
			})
		}),
	)
}

func (u *UI) railButton(gtx C, c *widget.Clickable, active bool, glyph func(gtx C, col color.NRGBA) D, badge int, dot bool) D {
	p := u.pal
	return clickable(gtx, c, func(gtx C) D {
		sz := gtx.Dp(40)
		if active {
			fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, p.RailActiveBg)
		} else if c.Hovered() {
			fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, p.Hover)
		}
		col := p.RailIcon
		if active {
			col = p.RailActive
		}
		off := (sz - gtx.Dp(24)) / 2
		t := op.Offset(image.Pt(off, off)).Push(gtx.Ops)
		glyph(gtx, col)
		t.Pop()

		switch {
		case badge > 0:
			m := op.Record(gtx.Ops)
			bd := u.smallBadge(gtx, badge)
			call := m.Stop()
			t := op.Offset(image.Pt(sz-bd.Size.X+gtx.Dp(4), -gtx.Dp(2))).Push(gtx.Ops)
			call.Add(gtx.Ops)
			t.Pop()
		case dot:
			r := gtx.Dp(4)
			ring := gtx.Dp(1.5)
			ctr := image.Pt(sz-gtx.Dp(8), gtx.Dp(8))
			fillCircle(gtx, ctr, r+ring, p.Rail)
			fillCircle(gtx, ctr, r, p.Badge)
		}
		return D{Size: image.Pt(sz, sz)}
	})
}

// smallBadge is the compact count bubble shown on rail icons.
func (u *UI) smallBadge(gtx C, n int) D {
	h := gtx.Dp(18)
	gtx.Constraints.Min = image.Point{}
	m := op.Record(gtx.Ops)
	ld := u.label(unit.Sp(11), itoa(n), u.pal.BadgeText, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout(gtx)
	call := m.Stop()
	w := max(h, ld.Size.X+gtx.Dp(10))
	ring := gtx.Dp(2)
	fillRRect(gtx, image.Rect(-ring, -ring, w+ring, h+ring), h/2+ring, u.pal.Rail)
	fillRRect(gtx, image.Rect(0, 0, w, h), h/2, u.pal.Badge)
	t := op.Offset(image.Pt((w-ld.Size.X)/2, (h-ld.Size.Y)/2)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	return D{Size: image.Pt(w, h)}
}
