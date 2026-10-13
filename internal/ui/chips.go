package ui

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// layoutChips draws the filter chips (chipItems: All, Unread, Favourites,
// Groups, the custom lists and "+") in a row that, like WhatsApp's,
// scrolls sideways when they don't fit, with the wheel (either way) or a
// touchpad. The row's margins scroll with it, and a fade at either edge
// shows there are more chips that way.
func (u *UI) layoutChips(gtx C) D {
	p := u.pal
	return layout.Inset{Bottom: 10}.Layout(gtx, func(gtx C) D {
		pad, gap := gtx.Dp(21), gtx.Dp(8)
		cgtx := gtx
		cgtx.Constraints.Min = image.Point{}
		sel := &u.sidebar.chipSel
		items := u.chipItems()
		sel.step(gtx, u.activeChip(items), durSwitch)
		total, h := 2*pad, 0
		parts := make([]part, len(items))
		for i, it := range items {
			active := sel.of(i)
			fg := mix(p.ChipText, p.ChipActiveText, active)
			parts[i] = record(cgtx, func(gtx C) D {
				return u.chip(gtx, u.btn("chip:"+itoa(i)), active, func(gtx C) D {
					if it.add {
						// A round "+", as wide as it is tall.
						gtx.Constraints.Min.X = gtx.Dp(38)
						return layout.Center.Layout(gtx, iconW(icAdd, 22, p.ChipText))
					}
					return layout.Inset{Left: 12, Right: 12}.Layout(gtx,
						u.label(15, it.name, fg, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout)
				})
			})
			if i > 0 {
				total += gap
			}
			total += parts[i].size.X
			h = max(h, parts[i].size.Y)
		}
		w := gtx.Constraints.Max.X
		maxOff := max(0, total-w)

		// Claim only what the row can still scroll, so the rest goes on.
		off := &u.sidebar.chipOff
		rng := pointer.ScrollRange{Min: -*off, Max: maxOff - *off}
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: off, Kinds: pointer.Scroll, ScrollX: rng, ScrollY: rng})
			if !ok {
				break
			}
			if e, ok := ev.(pointer.Event); ok && e.Kind == pointer.Scroll {
				*off += int(e.Scroll.X + e.Scroll.Y)
			}
		}
		// A chip turned on (from New list, say) scrolls into view.
		if on := u.activeChip(items); on != u.sidebar.chipShown {
			u.sidebar.chipShown = on
			x0 := pad
			for _, pt := range parts[:on] {
				x0 += pt.size.X + gap
			}
			x1 := x0 + parts[on].size.X
			*off = min(*off, x0-pad)
			*off = max(*off, x1+pad-w)
		}
		*off = min(max(*off, 0), maxOff)

		size := image.Pt(w, h)
		cl := clip.Rect{Max: size}.Push(gtx.Ops)
		x := pad - *off
		for _, pt := range parts {
			pt.at(gtx, x, 0)
			x += pt.size.X + gap
		}
		fade := gtx.Dp(28)
		if *off > 0 {
			u.chipFade(gtx, 0, fade, h)
		}
		if *off < maxOff {
			u.chipFade(gtx, w, w-fade, h)
		}
		// On top of the chips, so the wheel reaches this; clicks pass.
		pass := pointer.PassOp{}.Push(gtx.Ops)
		event.Op(gtx.Ops, off)
		pass.Pop()
		cl.Pop()
		return D{Size: size}
	})
}

// chipFade fades the chips out towards an edge of the row: the panel's
// color at x0, nothing at x1.
func (u *UI) chipFade(gtx C, x0, x1, h int) {
	bg := u.pal.Panel
	none := color.NRGBA{R: bg.R, G: bg.G, B: bg.B}
	defer clip.Rect(image.Rect(min(x0, x1), 0, max(x0, x1), h)).Push(gtx.Ops).Pop()
	paint.LinearGradientOp{
		Stop1: f32.Pt(float32(x0), 0), Color1: bg,
		Stop2: f32.Pt(float32(x1), 0), Color2: none,
	}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}
