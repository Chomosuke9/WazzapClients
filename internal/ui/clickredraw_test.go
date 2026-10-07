package ui

import (
	"bytes"
	"image"
	"reflect"
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

// opsData is what a frame drew, to compare with another frame.
func opsData(ops *op.Ops) []byte {
	return bytes.Clone(reflect.ValueOf(ops).Elem().FieldByName("Internal").FieldByName("data").Bytes())
}

// TestClickRedraws clicks all over the window and checks that once a click
// stops asking for frames, the window shows everything it did: a frame
// more draws the same. Buttons that act while they're laid out, after the
// parts of the window they change, used to leave those as they were until
// the pointer moved.
func TestClickRedraws(t *testing.T) {
	if testing.Short() {
		t.Skip("clicks a few hundred places")
	}
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	u.SelectID("rina")
	now := testNow()
	var ops op.Ops
	var r input.Router
	frame := func() []byte {
		ops.Reset()
		u.Layout(layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1100, 700))})
		r.Frame(&ops)
		return opsData(&ops)
	}
	// settle draws frames until none is asked for at once, and returns the
	// last (nil if it keeps animating).
	settle := func() []byte {
		for range 60 {
			last := frame()
			if w, ok := r.WakeupTime(); !ok || !w.IsZero() {
				return last
			}
			now = now.Add(50 * time.Millisecond)
		}
		return nil
	}
	settle()
	var at time.Duration
	for y := 10; y < 700; y += 46 {
		for x := 10; x < 1100; x += 37 {
			p := f32.Pt(float32(x), float32(y))
			r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
			settle()
			at += time.Second
			r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p, Time: at})
			settle()
			r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p, Time: at + 50*time.Millisecond})
			if shown := settle(); shown != nil && !bytes.Equal(shown, frame()) {
				t.Errorf("a click at %d,%d left the window out of date", x, y)
			}
			r.WakeupTime()
			for range 2 {
				u.Escape()
				settle()
			}
		}
	}
}
