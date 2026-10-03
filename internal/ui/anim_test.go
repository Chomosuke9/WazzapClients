package ui

import (
	"image"
	"runtime"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
)

func TestTween(t *testing.T) {
	now := testNow()
	gtx := C{Now: now}
	var tw tween
	if v := tw.step(gtx, true, 100*time.Millisecond); v != 0 {
		t.Fatalf("first step moved to %v; it should count from this frame", v)
	}
	gtx.Now = now.Add(40 * time.Millisecond)
	if v := tw.step(gtx, true, 100*time.Millisecond); v < 0.39 || v > 0.41 {
		t.Fatalf("after 40 of 100ms: %v, want 0.4", v)
	}
	// Turning around goes back from where it is.
	gtx.Now = now.Add(60 * time.Millisecond)
	if v := tw.step(gtx, false, 100*time.Millisecond); v < 0.39 || v > 0.41 {
		t.Fatalf("reversing jumped to %v", v)
	}
	gtx.Now = now.Add(70 * time.Millisecond)
	if v := tw.step(gtx, false, 100*time.Millisecond); v < 0.29 || v > 0.31 {
		t.Fatalf("10ms after reversing: %v, want 0.3", v)
	}
	gtx.Now = now.Add(time.Second)
	if v := tw.step(gtx, false, 100*time.Millisecond); v != 0 {
		t.Fatalf("long after: %v, want 0", v)
	}
}

func TestFollower(t *testing.T) {
	now := testNow()
	gtx := C{Now: now}
	var f follower
	if v := f.step(gtx, 10, 100*time.Millisecond); v != 10 {
		t.Fatalf("first value %v, want a jump to 10", v)
	}
	f.step(gtx, 20, 100*time.Millisecond)
	gtx.Now = now.Add(50 * time.Millisecond)
	if v := f.step(gtx, 20, 100*time.Millisecond); v <= 10 || v >= 20 {
		t.Fatalf("midway %v, want between 10 and 20", v)
	}
	gtx.Now = now.Add(time.Second)
	if v := f.step(gtx, 20, 100*time.Millisecond); v != 20 {
		t.Fatalf("at the end %v, want 20", v)
	}
}

// TestFrameClock feeds the clock frames that start up to 1.5 ms before or
// after the refreshes they are shown at, as they do at 165 Hz, and checks
// that it hands out evenly spaced times close to the real ones.
func TestFrameClock(t *testing.T) {
	const period = 6060 * time.Microsecond
	base := testNow()
	jitter := []time.Duration{0, 1500, -1000, 1000, -500, 500, -1500, 0, 1200, -600}
	start := func(k int) time.Time {
		return base.Add(time.Duration(k)*period + jitter[k%len(jitter)]*time.Microsecond)
	}
	var c frameClock
	var prev time.Time
	k := 0
	for ; k < 200; k++ {
		now := start(k)
		got := c.next(now)
		if d := got.Sub(now); d > period/2 || d < -period/2 {
			t.Fatalf("frame %d: %v from the real time", k, d)
		}
		if k >= 30 {
			if step := got.Sub(prev); step < period-500*time.Microsecond || step > period+500*time.Microsecond {
				t.Fatalf("frame %d: step %v, want about %v", k, step, period)
			}
		}
		prev = got
	}
	// A late frame, shown a refresh later, moves on by two refreshes.
	k++
	got := c.next(start(k))
	if step := got.Sub(prev); step < 2*period-500*time.Microsecond || step > 2*period+500*time.Microsecond {
		t.Fatalf("after a late frame: step %v, want about %v", step, 2*period)
	}
	// After a pause the clock is the real time.
	now := start(k).Add(time.Second)
	if got := c.next(now); !got.Equal(now) {
		t.Fatalf("after a pause: %v from the real time", got.Sub(now))
	}
	// So is a frame time of zero (cmd/screenshot draws without a clock).
	if got := c.next(time.Time{}); !got.IsZero() {
		t.Fatalf("zero time became %v", got)
	}
}

// TestIdleAtRest checks that no animation keeps asking for frames once it
// has finished: an idle window must not redraw.
func TestIdleAtRest(t *testing.T) {
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	u.SelectID("rina")
	now := testNow()
	var ops op.Ops
	var r input.Router
	frame := func() {
		ops.Reset()
		u.Layout(layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1100, 700))})
		r.Frame(&ops)
		now = now.Add(time.Second)
	}
	move := func(x, y float32) {
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(x, y)})
	}
	steps := []struct {
		name string
		do   func()
	}{
		{"chat", func() {}},
		{"hover a chat", func() { move(200, 330) }},
		{"hover a message", func() { move(560, 250) }},
		{"info panel", func() { u.ShowInfo(0, 0) }},
		{"message menu", func() { u.ShowOverlay("msgmenu", 600, 300) }},
		{"close menu", u.Escape},
		{"emoji picker", func() { u.ShowOverlay("emoji", 0, 0) }},
		{"close picker", u.Escape},
		{"reply", func() { u.ShowOverlay("reply", 0, 0) }},
		{"select", func() { u.ShowOverlay("select", 0, 0) }},
		{"end select", u.Escape},
		{"viewer", func() { u.ShowOverlay("viewer", 600, 300) }},
		{"close viewer", u.Escape},
		{"delete dialog", func() { u.ShowOverlay("delete", 0, 0) }},
		{"close dialog", u.Escape},
		{"search panel", func() { u.ShowOverlay("search", 0, 0) }},
		{"close search", u.Escape},
		{"status page", func() { u.ShowPage("status") }},
		{"chats page", func() { u.ShowPage("chats") }},
		{"chat moves up", func() { b.Forward(b.Messages("rina", 1), []string{"gym"}) }},
		{"new message", func() { b.Forward(b.Messages("rina", 1), []string{"rina"}) }},
		{"ghost mode", func() { u.SetGhost(true) }},
		{"ghost mode off", func() { u.SetGhost(false) }},
	}
	for _, s := range steps {
		s.do()
		for range 5 {
			frame()
			r.WakeupTime() // reading clears it
		}
		frame()
		// A focused editor's caret blink asks for a frame later on; an
		// animation asks for the next one at once.
		if w, ok := r.WakeupTime(); ok && w.IsZero() {
			t.Errorf("%s: still redrawing at rest", s.name)
		}
	}
}

// TestWheelScroll checks that a wheel notch scrolls a list over a few
// frames instead of at once, and that scrolling up lets go of the newest
// message.
func TestWheelScroll(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("rina")
	now := testNow()
	var ops op.Ops
	var r input.Router
	frame := func(dt time.Duration) {
		ops.Reset()
		u.Layout(layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1100, 700))})
		r.Frame(&ops)
		now = now.Add(dt)
	}
	wheel := func(x, y, dy float32) {
		r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(x, y), Scroll: f32.Pt(0, dy)})
	}
	frame(time.Second)
	frame(time.Second)

	// Two notches 70 ms apart at 165 Hz. The list moves by whole pixels
	// each frame: those steps must grow and shrink gently, the second notch
	// included, rather than jump.
	sb := &u.sidebar.list.List
	const frameDt = 6060 * time.Microsecond
	ahead := func() float32 {
		if w := u.wheels[sb]; w != nil {
			return w.left + w.frac
		}
		return 0
	}
	var steps []float32
	var total, prev float32
	for i := range 100 {
		if i == 0 || i == 12 {
			wheel(200, 330, 120)
			prev += 120
		}
		frame(frameDt)
		a := ahead()
		steps = append(steps, prev-a)
		total += prev - a
		prev = a
	}
	if steps[0]+steps[1] <= 0 {
		t.Fatalf("the chat list didn't start moving in two frames: %v", steps[:2])
	}
	for i := 1; i < len(steps); i++ {
		if d := steps[i] - steps[i-1]; d > 5 || d < -5 {
			t.Fatalf("frame %d moved %v px after %v: a jump (steps %v)", i, steps[i], steps[i-1], steps[:i+1])
		}
	}
	if u.wheels[sb] != nil || total != 240 {
		t.Fatalf("after 0.6 s: moved %v of 240 px, still scrolling: %+v", total, u.wheels[sb])
	}

	// A precision touchpad's deltas, which aren't whole notches on
	// Windows, move the list at once.
	if runtime.GOOS == "windows" {
		start := sb.Position
		wheel(200, 330, 30)
		frame(frameDt)
		if sb.Position == start || u.wheels[sb] != nil {
			t.Fatalf("a touchpad scroll: list at %+v from %+v, left to ease: %+v", sb.Position, start, u.wheels[sb])
		}
	}

	conv := &u.conv.list.List
	if conv.Position.BeforeEnd {
		t.Fatal("the chat didn't open at its newest message")
	}
	wheel(560, 250, -120)
	for range 30 {
		frame(16 * time.Millisecond)
	}
	if !conv.Position.BeforeEnd {
		t.Fatal("scrolling up stayed at the newest message")
	}
}
