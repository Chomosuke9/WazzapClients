package ui

import (
	"image"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/op"
)

// Submenus: a context menu's row with an arrow (menuItem.more) opens
// another menu beside it, as in WhatsApp Web, on a click or while the
// pointer rests on it. With the keyboard Right or Enter goes in, Left or
// Esc comes back out to the row.

// subDelay is how long the pointer rests on a row before its submenu opens
// (or another's closes): on the way to a submenu it crosses other rows.
const subDelay = 150 * time.Millisecond

// submenu is the open submenu of the context menu.
type submenu struct {
	key   string     // of the row it belongs to; "" when there's none
	y     int        // how far down the menu that row is
	items []menuItem // as last drawn
	keys  bool       // the keyboard is in it

	hover string // the menu's row under the pointer
	since time.Time
	done  bool // hover did what it does
}

// submenuOf reports whether it, a row of menuPanel's prefix, is the one
// the open submenu belongs to.
func (u *UI) submenuOf(prefix string, it menuItem) bool {
	return prefix == "menu:" && it.more != nil && it.key == u.ctx.sub.key
}

// openSubmenu opens a row's submenu, and takes the focus there when it's
// on the row.
func (u *UI) openSubmenu(gtx C, it menuItem) {
	s := &u.ctx.sub
	s.key, s.items, s.keys = it.key, it.more(), false
	fi := gtx.Source.Focus()
	if !fi.Keyboard || fi.Tag != event.Tag(u.btn("menu:"+it.key)) {
		return
	}
	for _, sub := range s.items {
		if sub.key != "" {
			s.keys = true
			u.focusSoon(gtx, u.btn("sub:"+sub.key))
			return
		}
	}
}

// leaveSubmenu takes the keyboard out of the submenu, if it's in one, and
// back to its row.
func (u *UI) leaveSubmenu() bool {
	s := &u.ctx.sub
	if s.key == "" || !s.keys {
		return false
	}
	u.requestFocus(u.btn("menu:" + s.key))
	s.key, s.keys = "", false
	return true
}

// submenuKey is an arrow pressed in a menu with tag focused: it reports
// whether it went into a submenu or out of one.
func (u *UI) submenuKey(gtx C, tag event.Tag, name key.Name) bool {
	if !u.ctx.isOpen() {
		return false
	}
	switch name {
	case key.NameLeftArrow:
		return u.leaveSubmenu()
	case key.NameRightArrow:
		for _, it := range u.ctx.items {
			if it.more != nil && tag == event.Tag(u.btn("menu:"+it.key)) {
				u.openSubmenu(gtx, it)
				return true
			}
		}
		return u.ctx.sub.keys
	}
	return false
}

// updateSubmenu runs the action of a submenu's item clicked last frame.
func (u *UI) updateSubmenu(gtx C) {
	s := &u.ctx.sub
	if s.key == "" {
		return
	}
	found := false
	for _, it := range u.ctx.items {
		if it.key == s.key && it.more != nil {
			s.items, found = it.more(), true
		}
	}
	if !found {
		u.ctx.sub = submenu{} // its row went away
		return
	}
	for _, it := range s.items {
		if it.key == "" || !u.btn("sub:"+it.key).Clicked(gtx) {
			continue
		}
		if !it.stay {
			u.closeMenu()
		}
		if it.run != nil {
			it.run()
		}
		return
	}
}

// hoverSubmenu opens and closes submenus as the pointer rests on the
// menu's rows, which were just drawn.
func (u *UI) hoverSubmenu(gtx C) {
	m := &u.ctx
	if !m.isOpen() {
		return
	}
	s := &m.sub
	var row menuItem
	for _, it := range m.items {
		if it.key != "" && u.btn("menu:"+it.key).Hovered() {
			row = it
		}
	}
	if row.key != s.hover {
		s.hover, s.since, s.done = row.key, gtx.Now, false
	}
	if row.key == "" || s.done {
		return
	}
	if row.key == s.key || (s.key == "" && row.more == nil) {
		s.done = true
		return
	}
	if at := s.since.Add(subDelay); gtx.Now.Before(at) {
		gtx.Execute(op.InvalidateCmd{At: at})
		return
	}
	s.done = true
	s.key, s.keys = "", false
	if row.more != nil {
		u.openSubmenu(gtx, row)
		gtx.Execute(op.InvalidateCmd{}) // its row is drawn already, without its highlight
	}
}

// layoutSubmenu draws the open submenu beside menu, level with its row.
func (u *UI) layoutSubmenu(gtx C, menu image.Rectangle) {
	s := &u.ctx.sub
	if s.key == "" {
		return
	}
	if s.keys && u.ctx.isOpen() {
		focusTrap(gtx)
	}
	sz := gtx.Constraints.Max
	sub := record(gtx, func(gtx C) D { return u.menuPanel(gtx, "sub:", s.items) })
	x := menu.Max.X - gtx.Dp(8)
	if x+sub.size.X > sz.X {
		x = max(gtx.Dp(8), menu.Min.X-sub.size.X+gtx.Dp(8))
	}
	y := min(menu.Min.Y+s.y, sz.Y-sub.size.Y-gtx.Dp(8))
	sub.at(gtx, x, max(y, gtx.Dp(8)))
}
