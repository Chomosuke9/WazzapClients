// Copied from gioui.org/widget's Label v0.10.3 (SPDX-License-Identifier:
// Unlicense OR MIT) with one change: bitmap glyphs (color emoji) are
// offered to a hook first, as styledtext's TextStyle.Bitmap does, so they
// can be drawn animated or from the app's own pictures.

package ui

import (
	"image"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"golang.org/x/image/math/fixed"
)

// bitmapHook may draw a bitmap glyph g itself at r (relative to its
// line's origin), and reports whether it did.
type bitmapHook func(gtx C, g text.Glyph, r image.Rectangle) bool

// layoutLabel draws l as material.LabelStyle.Layout does (without a
// selectable State), offering its bitmap glyphs to bitmap.
func layoutLabel(gtx C, l material.LabelStyle, bitmap bitmapHook) D {
	m := op.Record(gtx.Ops)
	paint.ColorOp{Color: l.Color}.Add(gtx.Ops)
	col := m.Stop()
	return hookLabel{
		Alignment:       l.Alignment,
		MaxLines:        l.MaxLines,
		Truncator:       l.Truncator,
		WrapPolicy:      l.WrapPolicy,
		LineHeight:      l.LineHeight,
		LineHeightScale: l.LineHeightScale,
		bitmap:          bitmap,
	}.Layout(gtx, l.Shaper, l.Font, l.TextSize, l.Text, col)
}

// hookLabel is widget.Label with a bitmap hook.
type hookLabel struct {
	Alignment       text.Alignment
	MaxLines        int
	Truncator       string
	WrapPolicy      text.WrapPolicy
	LineHeight      unit.Sp
	LineHeightScale float32
	bitmap          bitmapHook
}

func (l hookLabel) Layout(gtx C, lt *text.Shaper, font font.Font, size unit.Sp, txt string, textMaterial op.CallOp) D {
	cs := gtx.Constraints
	textSize := fixed.I(gtx.Sp(size))
	lineHeight := fixed.I(gtx.Sp(l.LineHeight))
	lt.LayoutString(text.Parameters{
		Font:            font,
		PxPerEm:         textSize,
		MaxLines:        l.MaxLines,
		Truncator:       l.Truncator,
		Alignment:       l.Alignment,
		WrapPolicy:      l.WrapPolicy,
		MaxWidth:        cs.Max.X,
		MinWidth:        cs.Min.X,
		Locale:          gtx.Locale,
		LineHeight:      lineHeight,
		LineHeightScale: l.LineHeightScale,
	}, txt)
	m := op.Record(gtx.Ops)
	viewport := image.Rectangle{Max: cs.Max}
	it := labelIterator{
		viewport: viewport,
		maxLines: l.MaxLines,
		material: textMaterial,
		bitmap:   l.bitmap,
	}
	semantic.LabelOp(txt).Add(gtx.Ops)
	var glyphs [32]text.Glyph
	line := glyphs[:0]
	for g, ok := lt.NextGlyph(); ok; g, ok = lt.NextGlyph() {
		var ok bool
		if line, ok = it.paintGlyph(gtx, lt, g, line); !ok {
			break
		}
	}
	call := m.Stop()
	viewport.Min = viewport.Min.Add(it.padding.Min)
	viewport.Max = viewport.Max.Add(it.padding.Max)
	clipStack := clip.Rect(viewport).Push(gtx.Ops)
	call.Add(gtx.Ops)
	dims := layout.Dimensions{Size: it.bounds.Size()}
	dims.Size = cs.Constrain(dims.Size)
	dims.Baseline = dims.Size.Y - it.baseline
	clipStack.Pop()
	return dims
}

// labelIterator computes the bounding box of and paints text.
type labelIterator struct {
	viewport  image.Rectangle
	maxLines  int
	material  op.CallOp
	bitmap    bitmapHook
	linesSeen int
	lineOff   f32.Point
	padding   image.Rectangle
	bounds    image.Rectangle
	visible   bool
	first     bool
	baseline  int
}

func (it *labelIterator) processGlyph(g text.Glyph, ok bool) (visibleOrBefore bool) {
	if it.maxLines > 0 {
		if g.Flags&text.FlagLineBreak != 0 {
			it.linesSeen++
		}
		if it.linesSeen == it.maxLines && g.Flags&text.FlagParagraphBreak != 0 {
			return false
		}
	}
	if d := g.Bounds.Min.X.Floor(); d < it.padding.Min.X {
		it.padding.Min.X = d
	}
	if d := (g.Bounds.Max.X - g.Advance).Ceil(); d > it.padding.Max.X {
		it.padding.Max.X = d
	}
	if d := (g.Bounds.Min.Y + g.Ascent).Floor(); d < it.padding.Min.Y {
		it.padding.Min.Y = d
	}
	if d := (g.Bounds.Max.Y - g.Descent).Ceil(); d > it.padding.Max.Y {
		it.padding.Max.Y = d
	}
	logicalBounds := image.Rectangle{
		Min: image.Pt(g.X.Floor(), int(g.Y)-g.Ascent.Ceil()),
		Max: image.Pt((g.X + g.Advance).Ceil(), int(g.Y)+g.Descent.Ceil()),
	}
	if !it.first {
		it.first = true
		it.baseline = int(g.Y)
		it.bounds = logicalBounds
	}
	above := logicalBounds.Max.Y < it.viewport.Min.Y
	below := logicalBounds.Min.Y > it.viewport.Max.Y
	left := logicalBounds.Max.X < it.viewport.Min.X
	right := logicalBounds.Min.X > it.viewport.Max.X
	it.visible = !above && !below && !left && !right
	if it.visible {
		it.bounds.Min.X = min(it.bounds.Min.X, logicalBounds.Min.X)
		it.bounds.Min.Y = min(it.bounds.Min.Y, logicalBounds.Min.Y)
		it.bounds.Max.X = max(it.bounds.Max.X, logicalBounds.Max.X)
		it.bounds.Max.Y = max(it.bounds.Max.Y, logicalBounds.Max.Y)
	}
	return ok && !below
}

func (it *labelIterator) paintGlyph(gtx C, shaper *text.Shaper, glyph text.Glyph, line []text.Glyph) ([]text.Glyph, bool) {
	visibleOrBefore := it.processGlyph(glyph, true)
	if it.visible {
		if len(line) == 0 {
			it.lineOff = f32.Point{X: float32(glyph.X) / 64, Y: float32(glyph.Y)}.Sub(layout.FPt(it.viewport.Min))
		}
		line = append(line, glyph)
	}
	if glyph.Flags&text.FlagLineBreak != 0 || cap(line)-len(line) == 0 || !visibleOrBefore {
		t := op.Affine(f32.AffineId().Offset(it.lineOff)).Push(gtx.Ops)
		path := shaper.Shape(line)
		outline := clip.Outline{Path: path}.Op().Push(gtx.Ops)
		it.material.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		outline.Pop()
		t.Pop()
		it.paintBitmaps(gtx, shaper, line)
		line = line[:0]
	}
	return line, visibleOrBefore
}

// paintBitmaps paints the bitmap glyphs of a line, offering each to
// it.bitmap first, under a whole-pixel offset (a clip under a fractional
// one becomes a path). Those it doesn't draw are painted by the shaper,
// in runs: Shaper.Bitmaps places glyphs from the first one's X.
func (it *labelIterator) paintBitmaps(gtx C, shaper *text.Shaper, line []text.Glyph) {
	if len(line) == 0 {
		return
	}
	if it.bitmap == nil {
		t := op.Affine(f32.AffineId().Offset(it.lineOff)).Push(gtx.Ops)
		if call := shaper.Bitmaps(line); call != (op.CallOp{}) {
			call.Add(gtx.Ops)
		}
		t.Pop()
		return
	}
	defer op.Offset(image.Pt(int(it.lineOff.X+0.5), int(it.lineOff.Y+0.5))).Push(gtx.Ops).Pop()
	flush := func(run []text.Glyph) {
		if len(run) == 0 {
			return
		}
		call := shaper.Bitmaps(run)
		if call == (op.CallOp{}) {
			return
		}
		t := op.Offset(image.Pt((run[0].X - line[0].X).Round(), 0)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		t.Pop()
	}
	start := 0
	for i, g := range line {
		at := image.Pt((g.X - line[0].X + g.Offset.X).Round(), (g.Offset.Y + g.Bounds.Min.Y).Round())
		size := image.Pt((g.Bounds.Max.X - g.Bounds.Min.X).Round(), (g.Bounds.Max.Y - g.Bounds.Min.Y).Round())
		if size.X <= 0 || size.Y <= 0 || !it.bitmap(gtx, g, image.Rectangle{Min: at, Max: at.Add(size)}) {
			continue
		}
		flush(line[start:i])
		start = i + 1
	}
	flush(line[start:])
}
