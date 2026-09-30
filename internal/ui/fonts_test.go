package ui

import (
	"fmt"
	"image"
	"os"
	"strings"
	"testing"

	"gioui.org/font"
	"gioui.org/font/opentype"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"github.com/go-text/typesetting/fontscan"
)

// TestEmojiSpace checks that a space after an emoji is an ordinary space,
// not the emoji font's wide one.
func TestEmojiSpace(t *testing.T) {
	u := New(nil)
	checkEmojiSpaces(t, u)
}

// Linux commonly has an unpatched Noto Color Emoji installed. Gio loads system
// fonts before our collection, so simulate that order without depending on the
// test machine's installed fonts.
func TestEmojiSpaceWithSystemNoto(t *testing.T) {
	data, err := os.ReadFile("fonts/NotoColorEmoji.ttf")
	if err != nil {
		t.Fatal(err)
	}
	systemEmoji, err := opentype.ParseCollection(data)
	if err != nil {
		t.Fatal(err)
	}
	// Give each font its own location, as Gio does for installed fonts.
	// WithCollection alone gives duplicate families the same location and
	// replaces their cached face, which would hide the system-font collision.
	fm := fontscan.NewFontMap(nil)
	for i, f := range append(systemEmoji, bundledFonts()...) {
		fm.AddFace(f.Face.Face(), fontscan.Location{File: fmt.Sprintf("font-%d", i)},
			opentype.FontToDescription(f.Font))
	}
	fm.SetQuery(fontscan.Query{
		Families: strings.Split(string(typeface), ", "),
		Aspect:   opentype.FontToDescription(font.Font{}).Aspect,
	})
	normal := fm.ResolveFace('x')
	emoji := fm.ResolveFace('🎓')
	normalGlyph, normalOK := normal.NominalGlyph(' ')
	emojiGlyph, emojiOK := emoji.NominalGlyph(' ')
	if !normalOK || !emojiOK {
		t.Fatal("missing space glyph")
	}
	space := normal.HorizontalAdvance(normalGlyph) / float32(normal.Upem())
	emojiSpace := emoji.HorizontalAdvance(emojiGlyph) / float32(emoji.Upem())
	if emojiSpace <= 0 || emojiSpace > space*1.5 {
		t.Errorf("space in selected emoji font is %.2fem, want about %.2fem", emojiSpace, space)
	}
}

func checkEmojiSpaces(t *testing.T, u *UI) {
	t.Helper()
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(1<<20, 1<<20)}}
	width := func(s string) int {
		gtx.Ops.Reset()
		return u.label(40, s, u.pal.Text).Layout(gtx).Size.X
	}
	for _, emoji := range []string{"🎓", "👩‍💻", "👍🏽", "🇮🇩"} {
		for _, separator := range []string{" ", "\u00a0"} {
			space := width("x"+separator+"x") - width("xx")
			emojiSpace := width(emoji+separator+"A") - width(emoji+"A")
			if emojiSpace <= 0 || emojiSpace > space*3/2 {
				t.Errorf("space %q after %s is %dpx, want about %dpx", separator, emoji, emojiSpace, space)
			}
		}
	}
}
