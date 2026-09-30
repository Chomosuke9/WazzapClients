package ui

import (
	"math"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// Smooth wheel scrolling. Gio's List moves by a whole wheel notch (120px on
// Windows) in one frame. wheelList takes the wheel events before the list
// (and its scrollbar) does and hands the distance to it over a few frames,
// easing out like WhatsApp Desktop. Touch drags still go to the list.

// wheelTau is the time constant of the ease: a notch is 95% done after 3τ.
const wheelTau = 45 * time.Millisecond

// wheelScroll is a list's wheel distance still to scroll. Only lists that
// are moving have one.
type wheelScroll struct {
	pending float32         // px, positive towards the end
	last    time.Time       // frame that last moved the list
	at      layout.Position // where that frame left the list
}

// wheelList lays out list l with lay, smoothing its mouse wheel scrolling.
func (u *UI) wheelList(gtx C, l *layout.List, lay layout.Widget) D {
	if l.Axis != layout.Vertical {
		return lay(gtx)
	}
	if u.wheels == nil {
		u.wheels = make(map[*layout.List]*wheelScroll)
	}
	w := u.wheels[l]
	if w != nil && l.Position != w.at {
		// Moved by something else (a jump to a message, a chat switch).
		delete(u.wheels, l)
		w = nil
	}

	// Claim only what the list can still scroll, so wheel events at its
	// ends go on to whatever is under it. The list clamps the rest.
	var pending float32
	if w != nil {
		pending = w.pending
	}
	pos := l.Position
	rng := pointer.ScrollRange{Min: -1e6, Max: 1e6}
	if pos.First == 0 {
		rng.Min = -max(0, pos.Offset+int(pending))
	}
	if !pos.BeforeEnd {
		rng.Max = max(0, -int(pending))
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: l, Kinds: pointer.Scroll, ScrollY: rng})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok || e.Kind != pointer.Scroll || e.Scroll.Y == 0 {
			continue
		}
		if w == nil {
			// Start moving this frame rather than the next.
			w = &wheelScroll{last: gtx.Now.Add(-16 * time.Millisecond), at: l.Position}
			u.wheels[l] = w
		}
		w.pending += e.Scroll.Y
	}

	if w != nil {
		step := w.pending // all at once without a clock (cmd/screenshot)
		if !gtx.Now.IsZero() {
			dt := gtx.Now.Sub(w.last).Seconds()
			step *= float32(1 - math.Exp(-dt/wheelTau.Seconds()))
		}
		w.last = gtx.Now
		// At least a pixel a frame, so the tail doesn't crawl.
		d := int(step)
		if d == 0 && step != 0 {
			d = int(math.Copysign(1, float64(step)))
		}
		if d != 0 {
			if d < 0 && l.ScrollToEnd && !l.Position.BeforeEnd {
				// Let go of the end, or the list snaps back to it.
				l.Position.BeforeEnd = true
			}
			l.Position.Offset += d
			w.pending -= float32(d)
		}
		if math.Abs(float64(w.pending)) < 1 {
			delete(u.wheels, l)
			w = nil
		} else {
			gtx.Execute(op.InvalidateCmd{})
		}
	}

	dims := lay(gtx)

	if w != nil {
		atStart := l.Position.First == 0 && l.Position.Offset <= 0
		atEnd := !l.Position.BeforeEnd
		if atStart && w.pending < 0 || atEnd && w.pending > 0 {
			delete(u.wheels, l)
		}
		w.at = l.Position
	}
	// On top of the list, so the wheel reaches this first; clicks pass.
	defer clip.Rect{Max: dims.Size}.Push(gtx.Ops).Pop()
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, l)
	return dims
}
