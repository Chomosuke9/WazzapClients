package ui

import (
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// TestZoomAtPointer checks that the wheel zooms toward the pointer: the
// spot of the picture under it stays under it.
func TestZoomAtPointer(t *testing.T) {
	var z zoomPan
	area := image.Rect(100, 50, 1100, 850) // 1000x800
	pic := image.Pt(2000, 1600)            // fits the area exactly
	var ops op.Ops
	var r input.Router
	var dst image.Rectangle
	frame := func() {
		ops.Reset()
		// A zero Now makes the glides jump to their targets.
		gtx := layout.Context{Ops: &ops, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1200, 900))}
		z.update(gtx)
		dst = z.layout(gtx, area, pic)
		r.Frame(&ops)
	}
	// where returns the spot of the picture (0 to 1) at window point p.
	where := func(p f32.Point) f32.Point {
		return f32.Pt((p.X-float32(dst.Min.X))/float32(dst.Dx()), (p.Y-float32(dst.Min.Y))/float32(dst.Dy()))
	}
	near := func(a, b f32.Point) bool {
		d := a.Sub(b)
		return d.X*d.X+d.Y*d.Y < 0.002*0.002
	}
	wheel := func(p f32.Point, notches float32) {
		r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: p, Scroll: f32.Pt(0, -120*notches)})
		frame()
	}
	frame()
	if dst != area {
		t.Fatalf("fitted at %v", dst)
	}
	p := f32.Pt(850, 350)
	before := where(p)
	for i := 0; i < 5; i++ {
		wheel(p, 1)
		if got := where(p); !near(got, before) {
			t.Fatalf("after %d notches the pointer is over %v, was over %v", i+1, got, before)
		}
	}
	// Moving the pointer and zooming further keeps the new spot.
	p2 := f32.Pt(300, 600)
	before = where(p2)
	wheel(p2, 2)
	if got := where(p2); !near(got, before) {
		t.Fatalf("second spot moved from %v to %v", before, got)
	}
	// Zoomed in, the picture covers the area: no gap at any edge.
	if dst.Min.X > area.Min.X || dst.Max.X < area.Max.X || dst.Min.Y > area.Min.Y || dst.Max.Y < area.Max.Y {
		t.Fatalf("zoomed picture %v leaves a gap in %v", dst, area)
	}
	// Zooming all the way out centers it again.
	wheel(p, -40)
	if dst != area {
		t.Fatalf("zoomed out to %v", dst)
	}
}
