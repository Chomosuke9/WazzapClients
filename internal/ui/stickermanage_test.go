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

// Dragging a pack by its handle below the next one swaps them, in the
// dialog and in the backend's order, and the trash button asks before it
// takes one off.
func TestMyPacksReorder(t *testing.T) {
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	u.SelectID("clara")
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
	u.ShowOverlay("mypacks", 0, 0)
	for range 30 {
		frame()
	}
	names := func() string {
		var s string
		for _, p := range b.StickerPacks() {
			s += p.Name + ";"
		}
		return s
	}
	if got := names(); got != "Flump;Office cats;" {
		t.Fatalf("packs = %q", got)
	}
	mp := &u.dialog.myPacks
	// Find the first row's handle: the dialog is 520 wide, centered, and
	// the handle is the last 40 px button before its right padding. The
	// dialog covers the window's middle (a press outside would close it).
	x := float32((1100-520)/2 + 520 - 24 + 8 - 20)
	var y float32
	for y = 220; y < 480 && !mp.drag.on; y += 8 {
		p := f32.Pt(x, y)
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: p, Buttons: pointer.ButtonPrimary})
		frame()
		if mp.drag.on {
			break
		}
		r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p})
		frame()
	}
	if !mp.drag.on || mp.drag.id != "Flump" {
		t.Fatalf("no drag from the first handle: %+v", mp.drag)
	}
	for dy := float32(10); dy <= 120; dy += 10 {
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(x, y+dy), Buttons: pointer.ButtonPrimary})
		frame()
	}
	r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(x, y+120)})
	for range 30 {
		frame()
	}
	if mp.drag.on {
		t.Fatal("still dragging after the release")
	}
	if got := names(); got != "Office cats;Flump;" {
		t.Fatalf("after the drag: %q", got)
	}
	if mp.packs[0].Name != "Office cats" {
		t.Fatalf("the dialog's order: %s first", mp.packs[0].Name)
	}

	// The trash button asks in the row; Remove takes the pack off.
	mp.removing = mp.packs[0].ID
	u.btn("mypk:rm").Click()
	frame()
	if got := names(); got != "Flump;" || len(mp.packs) != 1 {
		t.Fatalf("after removing: %q, %d in the dialog", got, len(mp.packs))
	}
}
