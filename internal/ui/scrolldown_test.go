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

// Scrolling up shows the ⌄ button and pins the day at the top; messages
// that come meanwhile count on the button, and clicking it goes back down.
func TestScrollDown(t *testing.T) {
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	u.SelectID("rina") // read, so it opens at its newest message
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
	frame(time.Second)
	frame(time.Second)
	f := &u.conv.float
	if f.away || f.below != 0 {
		t.Fatalf("at the newest message: away %v, %d below", f.away, f.below)
	}

	r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(800, 400), Scroll: f32.Pt(0, -240)})
	for range 60 {
		frame(16 * time.Millisecond)
	}
	if !f.away {
		t.Fatalf("scrolled up, but no button: list at %+v", u.conv.list.Position)
	}
	if !f.pinned || f.dateAnim.v != 1 {
		t.Fatalf("scrolling: day pinned %v, shown %v", f.pinned, f.dateAnim.v)
	}
	for range 3 {
		frame(time.Second)
	}
	if f.dateAnim.v != 0 {
		t.Fatalf("the pinned day stayed after scrolling stopped: %v", f.dateAnim.v)
	}

	b.Receive("rina", "Rina", "Still there?")
	frame(time.Second)
	if f.below != 1 {
		t.Fatalf("a message came while scrolled up: %d below", f.below)
	}

	// Click the button, at the bottom right of the messages.
	at := f32.Pt(1100-18-21, float32(700-u.conv.composerH-14-21))
	r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: at, Buttons: pointer.ButtonPrimary},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: at})
	for range 5 {
		frame(100 * time.Millisecond)
	}
	if f.away || f.below != 0 {
		t.Fatalf("after clicking the button: away %v, %d below, list at %+v", f.away, f.below, u.conv.list.Position)
	}
}
