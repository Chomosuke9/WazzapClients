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
	"gioui.org/widget"
)

// TestScrollbarDrag checks that the thumb follows the pointer and that
// dragging it to the ends reaches the ends, with rows of uneven heights.
func TestScrollbarDrag(t *testing.T) {
	u := &UI{pal: &lightPalette}
	var l widget.List
	l.Axis = layout.Vertical
	const n, w, h = 300, 400, 600
	rowH := func(i int) int { return 20 + (i*37)%180 } // 20 to 199 px
	var ops op.Ops
	var r input.Router
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(w, h))}
		u.scrollList(gtx, &l, n, func(gtx C, i int) D { return D{Size: image.Pt(w, rowH(i))} })
		r.Frame(&ops)
	}
	press := func(kind pointer.Kind, y float32) {
		r.Queue(pointer.Event{Kind: kind, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(w-4, y)})
		frame()
	}
	frame()
	frame()
	b := u.bars[&l]
	if b.n != n || b.thumb <= 0 {
		t.Fatalf("no scrollbar: %+v", b)
	}
	// Grab the thumb by its middle and drag it down in steps.
	y := float32(b.top + b.thumb/2)
	press(pointer.Press, y)
	for _, to := range []float32{100, 250, 400, 520} {
		press(pointer.Move, to)
		if mid := b.top + b.thumb/2; abs(mid-int(to)) > 1 {
			t.Fatalf("dragged to %v, thumb middle at %v", to, mid)
		}
	}
	// Past the bottom: the list shows its last row.
	press(pointer.Move, h+200)
	if last := l.Position.First + l.Position.Count; last != n || l.Position.OffsetLast < 0 {
		t.Fatalf("dragged past the end, list at %+v", l.Position)
	}
	press(pointer.Release, h+200)
	if b.top+b.thumb != h-2 {
		t.Fatalf("after release at the end, thumb ends at %d, want %d", b.top+b.thumb, h-2)
	}
	// And back past the top.
	press(pointer.Press, float32(b.top+b.thumb/2))
	press(pointer.Move, -100)
	if l.Position.First != 0 || l.Position.Offset != 0 {
		t.Fatalf("dragged past the start, list at %+v", l.Position)
	}
	press(pointer.Release, -100)
	if b.top != 2 {
		t.Fatalf("thumb at %d at the start, want 2", b.top)
	}
	// The wheel still scrolls the list over the scrollbar.
	r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(w-4, 300), Scroll: f32.Pt(0, 120)})
	frame()
	if l.Position.First == 0 && l.Position.Offset == 0 {
		t.Fatalf("the wheel over the scrollbar didn't scroll")
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
