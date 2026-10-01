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

func TestFormatText(t *testing.T) {
	apply := func(s string, a, b int, k fmtKind) (string, string, bool) {
		r := []rune(s)
		e := formatText(r, a, b, k)
		out := append(append(append([]rune{}, r[:e.lo]...), e.repl...), r[e.hi:]...)
		return string(out), string(out[e.a:e.b]), e.remove
	}
	for _, c := range []struct {
		in        string
		a, b      int
		k         fmtKind
		out, sel  string
		wasStyled bool
	}{
		{"hello world", 6, 11, fmtBold, "hello *world*", "world", false},
		{"hello *world*", 7, 12, fmtBold, "hello world", "world", true}, // the inner text
		{"hello *world*", 6, 13, fmtBold, "hello world", "world", true}, // with its marks
		{"hello world", 0, 11, fmtItalic, "_hello world_", "hello world", false},
		{" hi ", 0, 4, fmtStrike, " ~hi~ ", "hi", false},             // marks hug the text
		{"ab\ncd", 0, 5, fmtItalic, "_ab_\n_cd_", "ab_\n_cd", false}, // per line
		{"_ab_\n_cd_", 1, 8, fmtItalic, "ab\ncd", "ab\ncd", true},
		{"*_~Test~_*", 3, 7, fmtBold, "_~Test~_", "Test", true}, // stacked marks
		{"*_~Test~_*", 3, 7, fmtItalic, "*~Test~*", "Test", true},
		{"*_~Test~_*", 3, 7, fmtStrike, "*_Test_*", "Test", true},
		{"*_~Test~_*", 0, 10, fmtItalic, "*~Test~*", "~Test~", true},
		{"x = 1", 0, 5, fmtCode, "`x = 1`", "x = 1", false},
		{"a\nb", 0, 3, fmtMono, "```a\nb```", "a\nb", false},
		{"a\nb", 0, 3, fmtQuote, "> a\n> b", "> a\n> b", false},
		{"> a\n> b", 2, 5, fmtQuote, "a\nb", "a\nb", true},
		{"one\ntwo\n", 0, 8, fmtList, "- one\n- two\n", "- one\n- two", false},
	} {
		out, sel, removed := apply(c.in, c.a, c.b, c.k)
		if out != c.out || sel != c.sel || removed != c.wasStyled {
			t.Errorf("%q [%d,%d) %s: got %q selecting %q (removed %v), want %q selecting %q (removed %v)",
				c.in, c.a, c.b, fmtButtons[c.k].label, out, sel, removed, c.out, c.sel, c.wasStyled)
		}
	}
}

// TestComposerClicks checks the composer's bigger click area, the
// formatting toolbar and double-clicking beside a message to reply.
func TestComposerClicks(t *testing.T) {
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
		now = now.Add(20 * time.Millisecond)
	}
	ev := func(k pointer.Kind, p f32.Point, b pointer.Buttons) {
		r.Queue(pointer.Event{Kind: k, Source: pointer.Mouse, Position: p, Buttons: b, Time: now.Sub(start)})
		frame()
	}
	click := func(p f32.Point) {
		ev(pointer.Press, p, pointer.ButtonPrimary)
		ev(pointer.Release, p, 0)
	}
	ed := &u.conv.composer
	u.requestFocus(nil)
	for range 30 {
		frame()
	}
	if r.Source().Focused(ed) {
		t.Fatal("the composer has the focus to begin with")
	}
	// The pill's padding, above the text line, at 1100x700.
	ev(pointer.Move, f32.Pt(800, 642), 0)
	click(f32.Pt(800, 642))
	if !r.Source().Focused(ed) {
		t.Error("a click above the composer's text didn't focus it")
	}

	ed.SetText("hello world")
	ed.SetCaret(6, 11)
	for range 20 {
		frame()
	}
	if u.conv.fmtAnim.v < 1 {
		t.Fatal("the formatting toolbar didn't open over the selection")
	}
	// The toolbar's first button, Bold, over "world".
	bold := f32.Pt(522, 610)
	ev(pointer.Move, bold, 0)
	click(bold)
	if got := ed.Text(); got != "hello *world*" {
		t.Errorf("Bold made %q", got)
	}
	if !u.conv.fmtActive[fmtBold] {
		frame()
		if !u.conv.fmtActive[fmtBold] {
			t.Error("Bold isn't shown as active on the bold selection")
		}
	}

	now = now.Add(time.Second)
	// Far right of the incoming "Deal" bubble.
	ev(pointer.Move, f32.Pt(900, 541), 0)
	click(f32.Pt(900, 541))
	click(f32.Pt(900, 541))
	if u.conv.reply == nil {
		t.Error("a double click beside a message didn't reply to it")
	}
}

// TestComposerFlags checks which parts of the composer text are drawn as
// marks and which in a style.
func TestComposerFlags(t *testing.T) {
	u := New(mock.New())
	txt := "*_~Test~_* a ```b``` c"
	fl := u.composerFlags(txt)
	var marks, styled []byte
	for i := range txt {
		switch {
		case fl[i]&cMark != 0:
			marks = append(marks, txt[i])
		case fl[i] != 0:
			styled = append(styled, txt[i])
		}
	}
	if string(marks) != "*_~~_*``````" {
		t.Errorf("marks %q", marks)
	}
	if string(styled) != "Testb" {
		t.Errorf("styled %q", styled)
	}
	for i := 3; i < 7; i++ {
		if want := uint8(styleBold | styleItalic | styleStrike); fl[i] != want {
			t.Errorf("%q has flags %b, want %b", txt[i], fl[i], want)
		}
	}
}
