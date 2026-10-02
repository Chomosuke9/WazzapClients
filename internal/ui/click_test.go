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

// TestClickThroughFade checks that an overlay fading out doesn't swallow
// clicks: a chat under a closing menu or viewer opens at once.
func TestClickThroughFade(t *testing.T) {
	for _, overlay := range []string{"chatmenu", "viewer"} {
		u := New(mock.New())
		u.Start(func() {})
		u.SelectID("rina") // the viewer shows one of its pictures
		now := testNow()
		var ops op.Ops
		var r input.Router
		frame := func() {
			ops.Reset()
			u.Layout(layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
				Constraints: layout.Exact(image.Pt(1100, 700))})
			r.Frame(&ops)
			now = now.Add(10 * time.Millisecond)
		}
		for range 3 {
			frame()
		}
		u.ShowOverlay(overlay, 150, 300)
		if overlay == "viewer" && !u.viewer.open {
			t.Fatal("the viewer didn't open")
		}
		for range 30 {
			frame()
		}
		u.Escape()
		frame()
		p := f32.Pt(250, 400) // a chat row, under the menu
		u.selected = nil
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: p, Buttons: pointer.ButtonPrimary},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p})
		frame()
		frame()
		if u.selected == nil {
			t.Errorf("%s: a click while it faded out opened nothing", overlay)
		}
	}
}
