package ui

import (
	"math"
	"runtime"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// Smooth wheel scrolling. Gio's List moves by a whole wheel notch (120px on
// Windows) in one frame. wheelList takes the wheel events before the list
// (and its scrollbar) does, and a critically damped spring pulls the list
// to where the notches point. Its speed builds up and dies down without
// jumps, and a notch that comes while it moves only moves where it heads,
// so a spinning wheel glides instead of lurching at each notch. Touchpads
// already scroll smoothly and move the list at once. Touch drags still go
// to the list.

// wheelSettle is how long the spring takes to cover 95% of a notch.
const wheelSettle = 150 * time.Millisecond

// wheelOmega is the spring's angular frequency (per second): a critically
// damped spring covers 95% of a step in 4.74/ω.
var wheelOmega = 4.74 / wheelSettle.Seconds()

// wheelLead is the time a scroll's first frame moves the spring by. It is
// shorter than any display's frame interval (4.2 ms at 240 Hz), so the
// first step is never bigger than the ones after it.
const wheelLead = 4 * time.Millisecond

// wheelRest is the speed (px/s) under which a spring within half a pixel
// of its target stops.
const wheelRest = 20

// wheelScroll is a list's wheel scroll in motion. Only lists that are
// moving have one.
type wheelScroll struct {
	left float32 // px from where the list really is to the target, positive towards the end
	vel  float32 // px/s, positive towards the end
	// frac is how far the list really is past its whole-pixel offset:
	// it moves by whole pixels and carries the rest.
	frac float32
	last time.Time       // frame that last moved the list
	at   layout.Position // where that frame left the list
}

// step moves the spring on by dt seconds and returns the distance it
// covered. It is the exact solution, so frames of any length move it alike.
func (w *wheelScroll) step(dt float64) float32 {
	om := wheelOmega
	e, de := float64(w.left), -float64(w.vel) // distance left, and its rate
	c := de + om*e
	k := math.Exp(-om * dt)
	e1 := (e + c*dt) * k
	de1 := (de - om*c*dt) * k
	w.left, w.vel = float32(e1), float32(-de1)
	return float32(e - e1)
}

// wheelNotch reports whether a scroll of d px comes from a mouse wheel's
// notches, which the spring eases in. Windows sends multiples of 120 for
// those, and any amount from a precision touchpad. Other systems' deltas
// are all eased.
func wheelNotch(d float32) bool {
	return runtime.GOOS != "windows" || math.Mod(float64(d), 120) == 0
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
	var ahead float32 // px still to move
	if w != nil {
		ahead = w.left + w.frac
	}
	pos := l.Position
	rng := pointer.ScrollRange{Min: -1e6, Max: 1e6}
	if pos.First == 0 {
		rng.Min = -max(0, pos.Offset+int(ahead))
	}
	if !pos.BeforeEnd {
		rng.Max = max(0, -int(ahead))
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
			w = &wheelScroll{last: gtx.Now.Add(-wheelLead), at: l.Position}
			u.wheels[l] = w
		}
		if wheelNotch(e.Scroll.Y) {
			w.left += e.Scroll.Y
		} else {
			w.frac += e.Scroll.Y
		}
	}

	if w != nil {
		if gtx.Now.IsZero() {
			// No clock (cmd/screenshot): all at once.
			w.frac += w.left
			w.left, w.vel = 0, 0
		} else {
			w.frac += w.step(max(0, gtx.Now.Sub(w.last).Seconds()))
		}
		w.last = gtx.Now
		done := math.Abs(float64(w.left)) < 0.5 && math.Abs(float64(w.vel)) < wheelRest
		if done {
			w.frac += w.left
			w.left = 0
		}
		if d := int(math.Round(float64(w.frac))); d != 0 {
			if d < 0 && l.ScrollToEnd && !l.Position.BeforeEnd {
				// Let go of the end, or the list snaps back to it.
				l.Position.BeforeEnd = true
			}
			l.Position.Offset += d
			w.frac -= float32(d)
		}
		if done {
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
		if ahead := w.left + w.frac; atStart && ahead < 0 || atEnd && ahead > 0 {
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

// keepVisible scrolls list l, whose rows have one height and of which
// rows show at once, just enough that row i shows whole: a picker's
// highlighted row as the arrow keys move it.
func keepVisible(l *layout.List, i, rows int) {
	p := &l.Position
	switch {
	case i < p.First || i == p.First && p.Offset > 0:
		p.First, p.Offset = i, 0
	case i >= p.First+rows:
		p.First, p.Offset = i-rows+1, 0
	}
}
