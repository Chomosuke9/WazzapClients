package ui

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// menuState is the chat list's ⋮ drop-down, WhatsApp Desktop's minus
// Select chats and App lock, plus Switch account.
type menuState struct {
	open       bool
	anim       tween
	anchor     image.Point // top-right corner, in window coordinates below the title bar
	scrim      widget.Clickable
	newGroup   widget.Clickable
	starred    widget.Clickable
	readAll    widget.Clickable
	switchAcct widget.Clickable // opens the account switcher beside the menu
	logout     widget.Clickable
}

func (u *UI) updateMenu(gtx C) {
	m := &u.menu
	if m.newGroup.Clicked(gtx) {
		u.openNewGroup(nil)
		m.open = false
	}
	if m.starred.Clicked(gtx) {
		u.openStarred()
		m.open = false
	}
	if m.readAll.Clicked(gtx) {
		u.markAllRead()
		m.open = false
	}
	if m.logout.Clicked(gtx) {
		u.logout()
		m.open = false
	}
	if m.switchAcct.Clicked(gtx) {
		u.acctMenu.open = !u.acctMenu.open
	}
	if m.scrim.Clicked(gtx) {
		m.open = false
	}
}

// markAllRead marks every unread chat read, archived ones too.
func (u *UI) markAllRead() {
	var ids []string
	for _, c := range u.chats {
		if c.Unread != 0 {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) > 0 {
		u.backend.MarkRead(ids)
	}
}

// The ⋮ menu's row and divider heights.
const (
	menuRowH     = 42
	menuDividerH = 9
)

// layoutMenu draws the open menu over everything else. A transparent scrim
// underneath catches clicks outside it and closes it.
func (u *UI) layoutMenu(gtx C) {
	m := &u.menu
	v := m.anim.step(gtx, m.open, popDur(m.open))
	if v == 0 {
		return
	}
	p := u.pal
	if m.open {
		sgtx := gtx
		sgtx.Constraints = layout.Exact(gtx.Constraints.Max)
		m.scrim.Layout(sgtx, func(gtx C) D { return D{Size: gtx.Constraints.Max} })
	} else {
		var done func()
		gtx, done = fadeOut(gtx)
		defer done()
	}

	type menuEntry struct {
		click   *widget.Clickable
		label   string
		ic      *icon.Icon
		more    bool // opens a submenu
		divider bool
	}
	items := []menuEntry{
		{click: &m.newGroup, label: "New group", ic: icGroupAdd},
		{click: &m.starred, label: "Starred messages", ic: icStar},
		{click: &m.readAll, label: "Mark all as read", ic: icChats},
		{divider: true},
	}
	switchY := -1 // the Switch account row's top, below the menu's padding
	if len(u.accounts) > 0 {
		switchY = (len(items)-1)*gtx.Dp(menuRowH) + gtx.Dp(menuDividerH)
		items = append(items, menuEntry{click: &m.switchAcct, label: "Switch account", ic: icSwitchAccount, more: true})
	}
	items = append(items, menuEntry{click: &m.logout, label: "Log out", ic: icLogout})

	w := gtx.Dp(220)
	rec := op.Record(gtx.Ops)
	mgtx := gtx
	mgtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Constraints.Max.Y)}
	dims := layout.Inset{Top: 8, Bottom: 8, Left: 8, Right: 8}.Layout(mgtx, func(gtx C) D {
		var children []layout.FlexChild
		for _, it := range items {
			it := it
			if it.divider {
				children = append(children, layout.Rigid(func(gtx C) D {
					h := gtx.Dp(menuDividerH)
					y := h / 2
					fillRect(gtx, image.Rect(gtx.Dp(14), y, gtx.Constraints.Max.X-gtx.Dp(14), y+max(1, gtx.Dp(1))), p.PopupDivider)
					return D{Size: image.Pt(gtx.Constraints.Max.X, h)}
				}))
				continue
			}
			children = append(children, layout.Rigid(func(gtx C) D {
				return clickable(gtx, it.click, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					h := u.hover(gtx, it.click)
					if it.more && u.acctMenu.open {
						h = 1 // its submenu is open
					}
					bg := mix(p.Menu, p.MenuHover, h)
					return background(gtx, bg, 8, func(gtx C) D {
						return vcenter(gtx, gtx.Dp(menuRowH), func(gtx C) D {
							return layout.Inset{Left: 12, Right: 8}.Layout(gtx, func(gtx C) D {
								children := []layout.FlexChild{
									layout.Rigid(iconW(it.ic, 20, p.Icon)),
									layout.Rigid(layout.Spacer{Width: 14}.Layout),
									layout.Flexed(1, u.label(15, it.label, p.Text).Layout),
								}
								if it.more {
									children = append(children, layout.Rigid(iconW(icChevronRight, 20, p.Icon)))
								}
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
							})
						})
					})
				})
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
	call := rec.Stop()

	pos := image.Pt(m.anchor.X-dims.Size.X, m.anchor.Y)
	if switchY >= 0 {
		// The account switcher opens beside its row, to the right.
		rowY := pos.Y + gtx.Dp(8) + switchY
		u.acctMenu.anchor = image.Pt(pos.X+dims.Size.X+gtx.Dp(6), rowY-gtx.Dp(8))
	}
	defer op.Offset(pos).Push(gtx.Ops).Pop()
	defer pushPopup(gtx, v, image.Pt(dims.Size.X, 0)).Pop()
	r := gtx.Dp(12)
	rect := image.Rectangle{Max: dims.Size}
	fillRRect(gtx, rect.Add(image.Pt(0, gtx.Dp(2))).Inset(-gtx.Dp(1)), r, p.Shadow)
	fillRRect(gtx, rect, r, p.Menu)
	defer clip.UniformRRect(rect, r).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
}

// filterMenuState is the drop-down of filter chips that didn't fit.
type filterMenuState struct {
	open   bool
	anim   tween
	origin image.Point // chips row, in content coordinates
	anchor image.Point // more-chip, relative to origin
	scrim  widget.Clickable
	items  [len(filterNames)]widget.Clickable
}

func (u *UI) updateFilterMenu(gtx C) {
	f := &u.filterMenu
	if u.sidebar.more.Clicked(gtx) {
		f.open = !f.open
	}
	if f.scrim.Clicked(gtx) {
		f.open = false
	}
	for i := range f.items {
		if f.items[i].Clicked(gtx) {
			u.sidebar.filter = i
			u.sidebar.list.Position = layout.Position{}
			f.open = false
		}
	}
}

func (u *UI) layoutFilterMenu(gtx C) {
	f := &u.filterMenu
	open := f.open && len(u.sidebar.hiddenFilters) > 0
	v := f.anim.step(gtx, open, popDur(open))
	if v == 0 {
		return
	}
	p := u.pal
	if open {
		sgtx := gtx
		sgtx.Constraints = layout.Exact(gtx.Constraints.Max)
		f.scrim.Layout(sgtx, func(gtx C) D { return D{Size: gtx.Constraints.Max} })
	} else {
		var done func()
		gtx, done = fadeOut(gtx)
		defer done()
	}

	w := gtx.Dp(180)
	rec := op.Record(gtx.Ops)
	mgtx := gtx
	mgtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Constraints.Max.Y)}
	dims := layout.UniformInset(8).Layout(mgtx, func(gtx C) D {
		var children []layout.FlexChild
		for _, i := range u.sidebar.hiddenFilters {
			i := i
			children = append(children, layout.Rigid(func(gtx C) D {
				c := &f.items[i]
				return clickable(gtx, c, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					h := u.hover(gtx, c)
					if u.sidebar.filter == i {
						h = 1
					}
					bg := mix(p.Menu, p.MenuHover, h)
					return background(gtx, bg, 8, func(gtx C) D {
						return vcenter(gtx, gtx.Dp(40), func(gtx C) D {
							return layout.Inset{Left: 12, Right: 12}.Layout(gtx, u.label(15, filterNames[i], p.Text).Layout)
						})
					})
				})
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
	call := rec.Stop()
	pos := f.origin.Add(f.anchor)
	defer op.Offset(pos).Push(gtx.Ops).Pop()
	defer pushPopup(gtx, v, image.Point{}).Pop()
	r := gtx.Dp(12)
	rect := image.Rectangle{Max: dims.Size}
	fillRRect(gtx, rect.Add(image.Pt(0, gtx.Dp(2))).Inset(-gtx.Dp(1)), r, p.Shadow)
	fillRRect(gtx, rect, r, p.Menu)
	defer clip.UniformRRect(rect, r).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
}
