package ui

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
)

// TestSelectText drags across a message's text, double-clicks a word,
// copies with Ctrl+C and clicks the selection away.
func TestSelectText(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("rina")
	now := time.Now()
	start := now
	var ops op.Ops
	var r input.Router
	frame := func() {
		ops.Reset()
		u.Layout(layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1100, 700))})
		r.Frame(&ops)
		now = now.Add(10 * time.Millisecond)
	}
	for range 30 {
		frame()
	}
	ev := func(k pointer.Kind, p f32.Point, b pointer.Buttons) {
		r.Queue(pointer.Event{Kind: k, Source: pointer.Mouse, Position: p, Buttons: b, Time: now.Sub(start)})
		frame()
	}
	click := func(p f32.Point) {
		ev(pointer.Press, p, pointer.ButtonPrimary)
		ev(pointer.Release, p, 0)
	}
	// The last message is "Also can you send me the villa address? My mom
	// keeps asking". Text widths depend on the system's fonts, so the
	// points to press are found from the text's carets: a click on the
	// text shows where it is in the window.
	probe := f32.Pt(560, 580)
	click(probe)
	if u.textSel.id == "" {
		t.Fatal("the click missed the message's text")
	}
	origin := probe.Sub(layout.FPt(u.textSel.lastPos))
	plain := []rune(string(u.textSel.plain))
	runeAt := func(s string) int {
		for i := range plain {
			if strings.HasPrefix(string(plain[i:]), s) {
				return i
			}
		}
		t.Fatalf("the text %q has no %q", string(plain), s)
		return 0
	}
	// caret returns the window point of the caret before rune i, moved by dx.
	caret := func(i int, dx float32) f32.Point {
		for _, c := range u.textSel.carets {
			if c.Rune == i {
				return origin.Add(f32.Pt(float32(c.X)+dx, float32(c.Top+c.Bottom)/2))
			}
		}
		t.Fatalf("no caret before rune %d", i)
		return f32.Point{}
	}
	from, to := caret(runeAt("can you"), 0), caret(runeAt("asking")+len("asking"), 0)
	villa := caret(runeAt("villa"), 4)
	now = now.Add(time.Second)
	ev(pointer.Move, from, 0)
	ev(pointer.Press, from, pointer.ButtonPrimary)
	ev(pointer.Move, to, pointer.ButtonPrimary)
	ev(pointer.Release, to, 0)
	const want = "can you send me the villa address? My mom keeps asking"
	if got := u.textSel.selected(u.textSel.id); got != want {
		t.Fatalf("the drag selected %q, want %q", got, want)
	}
	r.Queue(key.Event{Name: "C", Modifiers: key.ModShortcut, State: key.Press})
	frame()
	if u.pendingCopy != "" || u.textSel.selected(u.textSel.id) != want {
		t.Errorf("Ctrl+C didn't copy the selection")
	}

	now = now.Add(time.Second)
	click(villa)
	click(villa)
	if got := u.textSel.selected(u.textSel.id); got != "villa" {
		t.Errorf("the double click selected %q, want %q", got, "villa")
	}
	if u.conv.reply != nil {
		t.Error("the double click on the text started a reply")
	}

	now = now.Add(time.Second)
	click(f32.Pt(250, 400)) // a chat row
	if u.textSel.id != "" {
		t.Error("a click elsewhere kept the selection")
	}
}
