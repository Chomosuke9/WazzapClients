package ui

import (
	"image"
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
	now := time.Now()
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
	now := time.Now()
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

// TestIdleAtRest checks that no animation keeps asking for frames once it
// has finished: an idle window must not redraw.
func TestIdleAtRest(t *testing.T) {
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	u.SelectID("rina")
	now := time.Now()
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
		{"status page", func() { u.ShowPage("status") }},
		{"chats page", func() { u.ShowPage("chats") }},
		{"chat moves up", func() { b.Forward(b.Messages("rina", 1), []string{"gym"}) }},
		{"new message", func() { b.Forward(b.Messages("rina", 1), []string{"rina"}) }},
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
	now := time.Now()
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

	sb := &u.sidebar.list.List
	start := sb.Position
	wheel(200, 330, 120)
	frame(16 * time.Millisecond)
	w := u.wheels[sb]
	if w == nil || w.pending <= 0 || w.pending >= 120 {
		t.Fatalf("after one frame: %+v, want part of the notch left", w)
	}
	if sb.Position == start {
		t.Fatal("the chat list didn't start moving in the first frame")
	}
	for range 30 {
		frame(16 * time.Millisecond)
	}
	if u.wheels[sb] != nil {
		t.Fatalf("still scrolling after half a second: %+v", u.wheels[sb])
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
