package ui

import (
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// TestEmojiSpace checks that a space after an emoji is an ordinary space,
// not the emoji font's wide one.
func TestEmojiSpace(t *testing.T) {
	u := New(nil)
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(1<<20, 1<<20)}}
	width := func(s string) int {
		gtx.Ops.Reset()
		return u.label(40, s, u.pal.Text).Layout(gtx).Size.X
	}
	space := width("x x") - width("xx")
	emojiSpace := width("🎓 A") - width("🎓A")
	if emojiSpace > space*3/2 {
		t.Errorf("space after emoji is %dpx, want about %dpx", emojiSpace, space)
	}
}
