package ui

import (
	"image"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
)

// layoutChips draws the filter chips (chipItems: All, Unread, Favourites,
// Groups, the custom lists and "+"), each with the number of unread chats
// it holds. Chips that don't fit collapse, like WhatsApp's, into a round
// ▾ chip whose menu lists them (chipMenuItems).
func (u *UI) layoutChips(gtx C) D {
	p := u.pal
	return layout.Inset{Left: 21, Right: 21, Bottom: 10}.Layout(gtx, func(gtx C) D {
		gap, more := gtx.Dp(8), gtx.Dp(38)
		cgtx := gtx
		cgtx.Constraints.Min = image.Point{}
		sel := &u.sidebar.chipSel
		items := u.chipItems()
		counts := u.chipCounts(items)
		sel.step(gtx, u.activeChip(items), durSwitch)
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
					return layout.Inset{Left: 12, Right: 12}.Layout(gtx, func(gtx C) D {
						name := u.label(15, it.name, fg, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout
						if counts[i] == 0 {
							return name(gtx)
						}
						return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
							layout.Rigid(name),
							layout.Rigid(layout.Spacer{Width: 6}.Layout),
							layout.Rigid(u.label(13, itoa(counts[i]), fg, labelOpts{maxLines: 1}).Layout))
					})
				})
			})
		}

		// As many chips as fit, keeping room for ▾ if any are left over.
		avail := gtx.Constraints.Max.X
		shown, x := 0, 0
		for i, pt := range parts {
			need := pt.size.X
			if i < len(parts)-1 {
				need += gap + more
			}
			if x+need > avail && i >= 1 {
				break
			}
			x += pt.size.X + gap
			shown++
		}
		u.sidebar.chipHidden = shown
		x, h := 0, 0
		for i := 0; i < shown; i++ {
			parts[i].at(gtx, x, 0)
			x += parts[i].size.X + gap
			h = max(h, parts[i].size.Y)
		}
		if shown < len(parts) {
			var hiddenActive float32
			for i := shown; i < len(parts); i++ {
				hiddenActive = max(hiddenActive, sel.of(i))
			}
			t := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
			d := u.chip(cgtx, u.btn("chip:more"), hiddenActive, func(gtx C) D {
				gtx.Constraints.Min.X = more
				return layout.Center.Layout(gtx, func(gtx C) D {
					d := iconW(icDropDown, 22, p.ChipText)(gtx)
					withOpacity(gtx, hiddenActive, func() { iconW(icDropDown, 22, p.ChipActiveText)(gtx) })
					return d
				})
			})
			t.Pop()
			u.sidebar.chipMoreH = d.Size.Y
			x += d.Size.X
			h = max(h, d.Size.Y)
		}
		return D{Size: image.Pt(min(x, avail), h)}
	})
}

// chipCounts counts the unread chats (archived ones aside) each chip
// holds; All shows none.
func (u *UI) chipCounts(items []chipItem) []int {
	counts := make([]int, len(items))
	var unread map[string]bool
	for _, c := range u.chats {
		if c.Unread == 0 || c.Archived {
			continue
		}
		if unread == nil {
			unread = make(map[string]bool)
		}
		unread[c.ID] = true
		for i, it := range items {
			switch {
			case it.add || it.filter == filterAll || it.filter == filterList:
			case it.filter == filterUnread,
				it.filter == filterFavorites && c.Favorite,
				it.filter == filterGroups && c.IsGroup:
				counts[i]++
			}
		}
	}
	if unread == nil {
		return counts
	}
	for i, it := range items {
		if it.filter != filterList || it.add {
			continue
		}
		for _, l := range u.lists {
			if l.ID == it.list {
				for _, id := range l.Chats {
					if unread[id] {
						counts[i]++
					}
				}
			}
		}
	}
	return counts
}

// openChipMenu opens the menu of the chips that didn't fit, under ▾.
func (u *UI) openChipMenu() {
	at := u.mouse
	if h := u.btn("chip:more").History(); len(h) > 0 {
		// The chip's bottom left: the click, less where it was in it.
		at = at.Sub(h[len(h)-1].Position).Add(image.Pt(0, u.sidebar.chipMoreH))
	}
	u.ctx = ctxMenu{kind: ctxChips, at: at}
}

// chipMenuItems are the chips that didn't fit, the one on ticked.
func (u *UI) chipMenuItems() []menuItem {
	items := u.chipItems()
	on := u.activeChip(items)
	var out []menuItem
	for i := u.sidebar.chipHidden; i < len(items); i++ {
		it := items[i]
		mi := menuItem{key: "chip:" + itoa(i), label: it.name, tick: i == on, run: func() { u.pickChip(it) }}
		if it.add {
			mi.ic = icAdd
		}
		out = append(out, mi)
	}
	return out
}
