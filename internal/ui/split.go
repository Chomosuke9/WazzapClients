package ui

import (
	"image"
	"strconv"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
)

// The list column (the chat list, or the open page's list in its place)
// can be dragged wider or narrower by its right edge, like WhatsApp's, and
// hidden to give the chat the whole window: by dragging the edge all the
// way left, from the list's ⋮ menu, by clicking the open page's rail
// button again, or with Ctrl+Shift+L. Both stick, as app preferences.

const (
	prefListWidth  = "list_width"  // the list's width in dp; the default if ""
	prefListHidden = "list_hidden" // "on" while the list is hidden
)

const (
	listMinW unit.Dp = 280
	listDefW unit.Dp = 430 // the most the default width grows to
	convMinW unit.Dp = 380 // the list grows no wider than leaves the chat this
)

// splitState is the list column's width and whether it shows.
type splitState struct {
	w      unit.Dp // the width the edge was dragged to; 0 for the default
	hidden bool
	anim   tween // 1 while the list shows
	drag   bool
	grab   int  // the pointer's X minus the edge's when the drag started
	hover  bool // the pointer is on the edge
	// pressed is when the edge was last pressed (event time), to tell a
	// double click.
	pressed time.Duration
}

// loadSplit reads the list's width and whether it's hidden.
func (u *UI) loadSplit() {
	if w, err := strconv.ParseFloat(u.backend.Pref(prefListWidth), 32); err == nil && w > 0 {
		u.split.w = unit.Dp(w)
	}
	u.split.hidden = u.backend.Pref(prefListHidden) == "on"
	u.split.anim.snap(!u.split.hidden)
}

// saveSplit stores the list's width and whether it's hidden.
func (u *UI) saveSplit() {
	w := ""
	if u.split.w > 0 {
		w = strconv.FormatFloat(float64(u.split.w), 'f', 1, 32)
	}
	u.backend.SetPref(prefListWidth, w)
	hidden := ""
	if u.split.hidden {
		hidden = "on"
	}
	u.backend.SetPref(prefListHidden, hidden)
}

// setListHidden hides or shows the list column.
func (u *UI) setListHidden(hidden bool) {
	if u.split.hidden == hidden {
		return
	}
	u.split.hidden = hidden
	u.saveSplit()
}

// listWidth is the list column's width in a pane pw wide.
func (u *UI) listWidth(gtx C, pw int) int {
	w := int(float32(pw) * 0.372)
	w = max(gtx.Dp(listMinW), min(w, gtx.Dp(listDefW)))
	if u.split.w > 0 {
		w = gtx.Dp(u.split.w)
	}
	return max(gtx.Dp(listMinW), min(w, pw-gtx.Dp(convMinW)))
}

// listShown reports how much of the list column shows, from 0 (hidden) to
// 1, and moves it toward its target. Something open in the list (the New
// chat panel) shows it.
func (u *UI) listShown(gtx C) float32 {
	if u.split.hidden && u.newChat.step != ncNone {
		u.setListHidden(false)
	}
	return easeOut(u.split.anim.step(gtx, !u.split.hidden, durPanel))
}

// splitKeys toggles the list with Ctrl+Shift+L.
func (u *UI) splitKeys(gtx C) {
	for {
		ev, ok := gtx.Event(key.Filter{Name: "L", Required: key.ModShortcut | key.ModShift})
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			u.setListHidden(!u.split.hidden)
		}
	}
}

// splitUpdate follows a drag of the list's edge, which is at edge (0 while
// the list is hidden) in a pane pw wide. Positions are in the pane's
// coordinates, which don't move while the edge does.
func (u *UI) splitUpdate(gtx C, pw, edge int) {
	s := &u.split
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: s,
			Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Enter | pointer.Leave})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		x := int(e.Position.X)
		switch e.Kind {
		case pointer.Enter:
			s.hover = true
		case pointer.Leave:
			s.hover = false
		case pointer.Press:
			if !e.Buttons.Contain(pointer.ButtonPrimary) {
				continue
			}
			double := s.pressed != 0 && e.Time-s.pressed < 400*time.Millisecond
			s.pressed = e.Time
			if double {
				// A double click puts the default width back.
				s.drag, s.w = false, 0
				u.setListHidden(false)
				u.saveSplit()
				continue
			}
			s.drag, s.grab = true, x-edge
		case pointer.Drag:
			if !s.drag {
				continue
			}
			w, least := x-s.grab, gtx.Dp(listMinW)
			// Past half the narrowest width, the list snaps shut (or,
			// dragged back out, open).
			s.hidden = w < least/2
			if !s.hidden {
				w = max(least, min(w, pw-gtx.Dp(convMinW)))
				s.w = unit.Dp(float32(w) / gtx.Metric.PxPerDp)
			}
		case pointer.Release, pointer.Cancel:
			if s.drag {
				s.drag = false
				u.saveSplit()
			}
		}
	}
}

// layoutSplitEdge registers the list's edge, at x in the pane, for
// dragging, and lights the divider while it's held or hovered.
func (u *UI) layoutSplitEdge(gtx C, x, h int) {
	s := &u.split
	v := u.anims.fade(gtx, animKey{p: s, tag: tagHover}, s.hover || s.drag, durHoverIn, durHoverOut)
	if v > 0 && x > 0 {
		col := faded(u.pal.TextSecondary, v*0.6)
		w := gtx.Dp(2)
		fillRect(gtx, image.Rect(x-w/2, 0, x-w/2+w, h), col)
	}
	area := clip.Rect(image.Rect(x-gtx.Dp(3), 0, x+gtx.Dp(5), h)).Push(gtx.Ops)
	pointer.CursorColResize.Add(gtx.Ops)
	event.Op(gtx.Ops, s)
	area.Pop()
}

// layoutSplit lays the list column and the right pane side by side in a
// pane of size sz, the list sliding out to the left while it hides.
func (u *UI) layoutSplit(gtx C, sz image.Point, list, right layout.Widget) D {
	listW := u.listWidth(gtx, sz.X)
	v := u.listShown(gtx)
	shown := lerpInt(0, listW, v)
	u.splitUpdate(gtx, sz.X, shown)
	// A drag may have moved the edge; lay out where it is now.
	listW = u.listWidth(gtx, sz.X)
	v = u.listShown(gtx)
	shown = lerpInt(0, listW, v)

	dw := max(1, gtx.Dp(1))
	if shown > 0 {
		c := clip.Rect{Max: image.Pt(shown, sz.Y)}.Push(gtx.Ops)
		off := op.Offset(image.Pt(shown-listW, 0)).Push(gtx.Ops)
		lgtx := gtx
		lgtx.Constraints = layout.Exact(image.Pt(listW, sz.Y))
		list(lgtx)
		off.Pop()
		c.Pop()
		fillRect(gtx, image.Rect(shown, 0, shown+dw, sz.Y), u.pal.Divider)
	}
	// While hidden, the card's own border is the edge.
	x := shown + dw
	off := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
	rgtx := gtx
	rgtx.Constraints = layout.Exact(image.Pt(sz.X-x, sz.Y))
	right(rgtx)
	off.Pop()
	u.layoutSplitEdge(gtx, shown, sz.Y)
	return D{Size: sz}
}
