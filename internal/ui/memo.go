package ui

import (
	"image"
	"image/color"
	"math"
	"strings"

	"gioui.org/f32"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/model"
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

// Shapes recorded once. Gio keys the GPU data of a path by where the path
// was recorded, so a path built into the frame's ops is tessellated and
// uploaded to a new GPU buffer on every frame, while one recorded in ops
// that outlive the frame is uploaded once and reused while it is drawn
// every frame (that is how text glyphs are cached). Only the scale part of
// the transform is in the key: a shape can be drawn anywhere.
type shapeKey struct {
	kind    shapeKind
	w, h, r int
}

type shapeKind uint8

const (
	shapeRound   shapeKind = iota // clip.UniformRRect at the origin
	shapeOutside                  // see roundCorner; w is the inset
)

var shapes struct {
	ops op.Ops
	m   map[shapeKey]clip.Op
}

// maxShapes bounds the shape cache; trimShapes empties it beyond that.
const maxShapes = 512

// roundShape returns a w×h rounded rectangle at the origin with radius r,
// recorded once. With r = w/2 = h/2 it is a circle.
func roundShape(w, h, r int) clip.Op {
	k := shapeKey{shapeRound, w, h, r}
	if c, ok := shapes.m[k]; ok {
		return c
	}
	c := clip.UniformRRect(image.Rect(0, 0, w, h), r).Op(&shapes.ops)
	putShape(k, c)
	return c
}

// outsideShape is the corner square at the origin without the quarter
// disc of radius rad whose square starts at (o, o); see roundCorner.
func outsideShape(o, rad int) clip.Op {
	k := shapeKey{shapeOutside, o, 0, rad}
	if c, ok := shapes.m[k]; ok {
		return c
	}
	// The same curve as clip.RRect.
	const iq = 1 - 4*(math.Sqrt2-1)/3
	of, rf := float32(o), float32(rad)
	var p clip.Path
	p.Begin(&shapes.ops)
	p.MoveTo(f32.Pt(0, 0))
	p.LineTo(f32.Pt(of+rf, 0))
	p.LineTo(f32.Pt(of+rf, of))
	p.CubeTo(f32.Pt(of+rf*iq, of), f32.Pt(of, of+rf*iq), f32.Pt(of, of+rf))
	p.LineTo(f32.Pt(0, of+rf))
	p.Close()
	c := clip.Outline{Path: p.End()}.Op()
	putShape(k, c)
	return c
}

func putShape(k shapeKey, c clip.Op) {
	if shapes.m == nil {
		shapes.m = make(map[shapeKey]clip.Op)
	}
	shapes.m[k] = c
}

// trimShapes empties the shape cache once it has grown past maxShapes
// (shapes of sizes that went away). It runs before a frame is laid out:
// the shapes are referenced from the frame's ops until it is drawn.
func trimShapes() {
	if len(shapes.m) > maxShapes {
		dropShapes()
	}
}

func dropShapes() {
	shapes.m = nil
	shapes.ops = op.Ops{}
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
		if (c < 0x20 && c != '\n') || c == 0x7f || (c == 0xe2 && i+2 < len(s) && s[i+1] == 0x81 && (s[i+2] == 0xa8 || s[i+2] == 0xa9 || s[i+2] == 0xa2 || s[i+2] == 0xa3)) {
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
		case r < 0x20, r == 0x7f, r == mentionStart, r == mentionEnd, r == model.MentionNotifies, r == model.MentionAdmins:
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
