package ui

import (
	"image/color"
	"strings"

	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/ui/styledtext"
)

// Caches for work that would otherwise be redone, and reallocated, on every
// frame. Frames come at display rate while the mouse moves, so per-frame
// garbage decides how big the heap grows between collections.

// glyphKey identifies one rendering of a hand-drawn glyph.
type glyphKey struct {
	name    string
	px      int
	col, bg color.NRGBA
	flag    bool
}

type glyphRec struct {
	ops  op.Ops
	call op.CallOp
	dims D
}

// glyphs holds recorded glyphs. It is only used from the window goroutine.
var glyphs = map[glyphKey]*glyphRec{}

// cachedGlyph draws a glyph recorded once per key. Stroked paths are the
// reason: clip.Stroke computes the outline on the CPU, allocating, each
// time its Op is built.
func cachedGlyph(gtx C, k glyphKey, draw func(gtx C) D) D {
	g, ok := glyphs[k]
	if !ok {
		g = new(glyphRec)
		m := op.Record(&g.ops)
		rg := gtx
		rg.Ops = &g.ops
		g.dims = draw(rg)
		g.call = m.Stop()
		glyphs[k] = g
	}
	g.call.Add(gtx.Ops)
	return g.dims
}

// memo is a string-keyed cache that is dropped when it grows too big,
// which is simpler than LRU and fine for texts on screen.
type memo[K comparable, V any] struct {
	m     map[K]V
	limit int
}

func (c *memo[K, V]) get(k K, f func() V) V {
	if v, ok := c.m[k]; ok {
		return v
	}
	if c.m == nil || len(c.m) >= c.limit {
		c.m = make(map[K]V)
	}
	v := f()
	c.m[k] = v
	return v
}

// displayText prepares text for shaping. No font draws control characters
// or the invisible mention marks, and when a text starts with one the font
// fallback loads an arbitrary system font for it: often a 20 MB CJK font
// that is then kept in memory. Tabs become spaces; newlines stay.
func displayText(s string) string {
	clean := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 0x20 && c != '\n') || c == 0x7f || (c == 0xe2 && i+2 < len(s) && s[i+1] == 0x81 && (s[i+2] == 0xa8 || s[i+2] == 0xa9)) {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case r == '\n':
			return r
		case r < 0x20, r == 0x7f, r == mentionStart, r == mentionEnd:
			return -1
		}
		return r
	}, s)
}

// previews caches chat list previews (formatting and mention marks
// removed, first line only).
var previews = memo[string, string]{limit: 1000}

func previewText(s string) string {
	return previews.get(s, func() string { return firstLine(plainText(s)) })
}

// richKey identifies a formatted text layout input.
type richKey struct {
	text   string
	size   unit.Sp
	col    color.NRGBA
	italic bool
	pills  pillFor
}

type richBlock struct {
	kind   blockKind
	marker string
	spans  []styledtext.SpanStyle
	deco   []spanDeco
}

var richBlocks = memo[richKey, []richBlock]{limit: 600}

// parsedRich returns a text's blocks with their styled spans, parsed once.
// The spans must not be modified.
func (u *UI) parsedRich(text string, size unit.Sp, col color.NRGBA, italic bool, pills pillFor) []richBlock {
	return richBlocks.get(richKey{text, size, col, italic, pills}, func() []richBlock {
		var out []richBlock
		for _, b := range parseBlocks(text) {
			spans, deco := u.richSpans(b.text, size, col, italic, pills)
			out = append(out, richBlock{b.kind, b.marker, spans, deco})
		}
		return out
	})
}
