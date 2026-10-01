package styledtext

import (
	"image"
	"testing"

	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
)

// TestNewlineHeight checks that line breaks inside and across spans add
// exactly one line each.
func TestNewlineHeight(t *testing.T) {
	sh := text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(500, 1000)}}
	tests := []struct {
		parts []string
		lines int
	}{
		{[]string{"a"}, 1},
		{[]string{"a\nb\nc"}, 3},
		{[]string{"x ", "B", "\nline2\nline3"}, 3},
		{[]string{"x ", "B", "\nline2\nline3", " \u00a0\u00a0"}, 3},
		{[]string{"x\n\ny"}, 3},
		{[]string{"héllö ✓\n\ny"}, 3}, // multi-byte runes before a blank line
		{[]string{"a\r\nb"}, 2},
	}
	for _, tt := range tests {
		var spans []SpanStyle
		for _, p := range tt.parts {
			spans = append(spans, SpanStyle{Font: font.Font{}, Size: 20, Content: p})
		}
		st := Text(sh, spans...)
		st.LineHeight, st.LineHeightScale = 22, 1
		if got := st.Layout(gtx, nil).Size.Y; got != 22*tt.lines {
			t.Errorf("%q: height %d, want %d", tt.parts, got, 22*tt.lines)
		}
	}
}

// TestLongWord checks that a word wider than the line breaks across lines,
// and that every line it takes counts toward the height.
func TestLongWord(t *testing.T) {
	sh := text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(200, 1000)}}
	long := "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ_0123456789abcdefghij"
	tests := []struct {
		parts []string
		min   int // fewest lines it can take
	}{
		{[]string{long}, 3},
		{[]string{"key: " + long + "\nnext"}, 4},
		{[]string{"key: ", long, "\nnext"}, 4},
		{[]string{"a\n\"" + long + "\",\nb\nc"}, 6},
	}
	for _, tt := range tests {
		var spans []SpanStyle
		for _, p := range tt.parts {
			spans = append(spans, SpanStyle{Font: font.Font{}, Size: 20, Content: p})
		}
		st := Text(sh, spans...)
		st.LineHeight, st.LineHeightScale = 22, 1
		d := st.Layout(gtx, func(gtx layout.Context, idx int, d layout.Dimensions) {
			if d.Size.X > 200 {
				t.Errorf("%q: span %d is %dpx wide, more than the line", tt.parts, idx, d.Size.X)
			}
		})
		if d.Size.Y < 22*tt.min || d.Size.Y%22 != 0 {
			t.Errorf("%q: height %d, want a multiple of 22, at least %d", tt.parts, d.Size.Y, 22*tt.min)
		}
	}
}
