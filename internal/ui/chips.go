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
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// The chat list's filter chips: All, Unread, Favourites, Groups, then the
// custom lists. Like WhatsApp's, the row scrolls sideways when they don't
// fit, with the wheel (either way) or a touchpad.

// chipRow is the filter chips' state besides the chosen filter.
type chipRow struct {
	off int // px scrolled
	// lists are the custom lists, read again after chat events (stale),
	// and in the chats of the list filtered on.
	lists []*model.ChatList
	stale bool
	in    map[string]bool
}

// loadLists reads the custom lists again when they may have changed.
func (u *UI) loadLists() {
	r := &u.sidebar.chipRow
	if r.lists != nil && !r.stale {
		return
	}
	r.stale = false
	r.lists = u.backend.Lists()
	if r.lists == nil {
		r.lists = []*model.ChatList{}
	}
	u.listFilterChats()
}

// listFilterChats notes the chats of the list filtered on, or drops the
// filter when the list is gone.
func (u *UI) listFilterChats() {
	r := &u.sidebar.chipRow
	clear(r.in)
	if u.sidebar.filter != filterList {
		return
	}
	for _, l := range r.lists {
		if l.ID == u.sidebar.listID {
			if r.in == nil {
				r.in = make(map[string]bool)
			}
			for _, id := range l.Chats {
				r.in[id] = true
			}
			return
		}
	}
	u.sidebar.filter, u.sidebar.listID = filterAll, ""
}

// updateChips handles clicks on the filter chips.
func (u *UI) updateChips(gtx C) {
	for i := range u.sidebar.chips {
		if u.sidebar.chips[i].Clicked(gtx) {
			u.sidebar.filter, u.sidebar.listID = i, ""
			u.sidebar.list.Position = layout.Position{}
		}
	}
	for _, l := range u.sidebar.chipRow.lists {
		if u.btn("chip:list:" + l.ID).Clicked(gtx) {
			u.sidebar.filter, u.sidebar.listID = filterList, l.ID
			u.listFilterChats()
			u.sidebar.list.Position = layout.Position{}
		}
	}
}

// chipKey is the chipSel key of the chip filtering on f (and list).
func (u *UI) chipKey(f int, list string) int {
	if f != filterList {
		return f
	}
	for i, l := range u.sidebar.chipRow.lists {
		if l.ID == list {
			return len(filterNames) + i
		}
	}
	return -1
}

// layoutChips draws the filter chips in a row that scrolls sideways. The
// row's margins scroll with it, and a fade at either edge shows there are
// more chips that way.
func (u *UI) layoutChips(gtx C) D {
	p := u.pal
	u.loadLists()
	r := &u.sidebar.chipRow
	return layout.Inset{Bottom: 10}.Layout(gtx, func(gtx C) D {
		pad, gap := gtx.Dp(21), gtx.Dp(8)
		cgtx := gtx
		cgtx.Constraints.Min = image.Point{}
		sel := &u.sidebar.chipSel
		sel.step(gtx, u.chipKey(u.sidebar.filter, u.sidebar.listID), durSwitch)
		parts := make([]part, 0, len(filterNames)+len(r.lists))
		chip := func(key int, click func() *widget.Clickable, name string) {
			active := sel.of(key)
			fg := mix(p.ChipText, p.ChipActiveText, active)
			parts = append(parts, record(cgtx, func(gtx C) D {
				return u.chip(gtx, click(), active, func(gtx C) D {
					return layout.Inset{Left: 12, Right: 12}.Layout(gtx,
						u.label(15, name, fg, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout)
				})
			}))
		}
		for i, name := range filterNames {
			chip(i, func() *widget.Clickable { return &u.sidebar.chips[i] }, name)
		}
		for i, l := range r.lists {
			chip(len(filterNames)+i, func() *widget.Clickable { return u.btn("chip:list:" + l.ID) }, l.Name)
		}
		total, h := 2*pad, 0
		for i, pt := range parts {
			if i > 0 {
				total += gap
			}
			total += pt.size.X
			h = max(h, pt.size.Y)
		}
		w := gtx.Constraints.Max.X
		maxOff := max(0, total-w)

		// The wheel scrolls the row either way; claim only what it can
		// still move, so the rest goes on.
		rng := pointer.ScrollRange{Min: -r.off, Max: maxOff - r.off}
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: r, Kinds: pointer.Scroll, ScrollX: rng, ScrollY: rng})
			if !ok {
				break
			}
			if e, ok := ev.(pointer.Event); ok && e.Kind == pointer.Scroll {
				r.off += int(e.Scroll.X + e.Scroll.Y)
			}
		}
		r.off = min(max(r.off, 0), maxOff)

		size := image.Pt(w, h)
		cl := clip.Rect{Max: size}.Push(gtx.Ops)
		x := pad - r.off
		for _, pt := range parts {
			pt.at(gtx, x, 0)
			x += pt.size.X + gap
		}
		fade := gtx.Dp(28)
		if r.off > 0 {
			u.chipFade(gtx, 0, fade, h)
		}
		if r.off < maxOff {
			u.chipFade(gtx, w, w-fade, h)
		}
		// On top of the chips, so the wheel reaches this; clicks pass.
		pass := pointer.PassOp{}.Push(gtx.Ops)
		event.Op(gtx.Ops, r)
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
	rect := image.Rect(min(x0, x1), 0, max(x0, x1), h)
	defer clip.Rect(rect).Push(gtx.Ops).Pop()
	paint.LinearGradientOp{
		Stop1: f32.Pt(float32(x0), 0), Color1: bg,
		Stop2: f32.Pt(float32(x1), 0), Color2: none,
	}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}
