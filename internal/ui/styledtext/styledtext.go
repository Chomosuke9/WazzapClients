// Copied from gioui.org/x/styledtext v0.10.3 (SPDX-License-Identifier: Unlicense OR MIT).

// Package styledtext provides rendering of text containing multiple fonts and styles.
package styledtext

import (
	"image"
	"image/color"
	"unicode/utf8"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"golang.org/x/image/math/fixed"
)

// SpanStyle describes the appearance of a span of styled text.
type SpanStyle struct {
	Font    font.Font
	Size    unit.Sp
	Color   color.NRGBA
	Content string

	idx     int
	runeOff int // runes of the text before Content
}

// Caret is a position between two clusters of the text, for selecting it.
type Caret struct {
	// Rune counts the runes of all spans before the caret.
	Rune int
	// X is the caret's position on its line, which spans Top to Bottom.
	X, Top, Bottom int
}

// spanShape describes the text shaping of a single span.
type spanShape struct {
	offset image.Point
	carets []Caret // relative to the shape, with Rune relative to the span
	call   op.CallOp
	size   image.Point
	ascent int
}

// Layout renders the span using the provided text shaping.
func (ss SpanStyle) Layout(gtx layout.Context, shape spanShape) layout.Dimensions {
	paint.ColorOp{Color: ss.Color}.Add(gtx.Ops)
	defer op.Offset(shape.offset).Push(gtx.Ops).Pop()
	shape.call.Add(gtx.Ops)
	return layout.Dimensions{Size: shape.size}
}

// WrapPolicy defines line wrapping policies for styledtext. Due to complexities
// of the styledtext implementation, there are fewer options available than in
// [gioui.org/text.WrapPolicy].
type WrapPolicy uint8

const (
	// WrapWords implements behavior like [gioui.org/text/.WrapWords]. This is the default,
	// as it prevents words from being split across lines.
	WrapWords WrapPolicy = iota
	// WrapWords implements behavior like [gioui.org/text/.WrapGraphemes]. This often gives
	// unpleasant results, as it will choose to split words across lines whenever it can. Some
	// use-cases may still want this, however.
	WrapGraphemes
)

func (s WrapPolicy) textPolicy() text.WrapPolicy {
	switch s {
	case WrapWords:
		return text.WrapWords
	default:
		return text.WrapGraphemes
	}
}

// TextStyle presents rich text.
type TextStyle struct {
	Styles     []SpanStyle
	Alignment  text.Alignment
	WrapPolicy WrapPolicy
	// LineHeight controls the distance between the baselines of lines of text.
	// If zero, a sensible default will be used.
	LineHeight unit.Sp
	// LineHeightScale applies a scaling factor to the LineHeight. If zero, a
	// sensible default will be used.
	LineHeightScale float32
	// Carets, if set, receives the position of every cluster boundary,
	// line by line from the top (left to right text only).
	Carets *[]Caret

	*text.Shaper
}

// Text constructs a TextStyle.
func Text(shaper *text.Shaper, styles ...SpanStyle) TextStyle {
	return TextStyle{
		Styles: styles,
		Shaper: shaper,
	}
}

type spanResults struct {
	call             op.CallOp
	carets           []Caret
	width            int
	height           int
	ascent           int
	runes            int
	multiLine        bool
	endedWithNewline bool
}

func (t TextStyle) iterateSpan(gtx layout.Context, maxWidth int, span SpanStyle, truncate bool, policy text.WrapPolicy) (op.CallOp, textIterator) {
	var glyphs [32]text.Glyph
	maxLines := 0
	if truncate {
		maxLines = 1
	}
	lineHeight := fixed.I(gtx.Sp(t.LineHeight))
	// shape the text of the current span
	macro := op.Record(gtx.Ops)
	t.Shaper.LayoutString(text.Parameters{
		Font:            span.Font,
		PxPerEm:         fixed.I(gtx.Sp(span.Size)),
		MaxLines:        maxLines,
		MaxWidth:        maxWidth,
		Truncator:       "\u200b", // Unicode zero-width space.
		Locale:          gtx.Locale,
		WrapPolicy:      policy,
		LineHeight:      lineHeight,
		LineHeightScale: t.LineHeightScale,
	}, span.Content)
	ti := textIterator{
		viewport: image.Rectangle{Max: gtx.Constraints.Max},
		maxLines: 1,
		color:    span.Color,
	}
	if t.Carets != nil {
		ti.carets = []Caret{{}}
	}

	line := glyphs[:0]
	for g, ok := t.Shaper.NextGlyph(); ok; g, ok = t.Shaper.NextGlyph() {
		line, ok = ti.paintGlyph(gtx, t.Shaper, g, line)
		if !ok {
			break
		}
	}
	return macro.Stop(), ti
}

func (t TextStyle) layoutSpan(gtx layout.Context, maxWidth int, span SpanStyle, lineEmpty bool) spanResults {
	call, ti := t.iterateSpan(gtx, maxWidth, span, true, t.WrapPolicy.textPolicy())
	runesDisplayed := ti.runes
	multiLine := runesDisplayed < utf8.RuneCountInString(span.Content)
	endedWithNewline := ti.hasNewline
	if multiLine {
		// Skip runesDisplayed runes (not bytes: multi-byte text, like
		// CJK, would otherwise stop early and miss a hard newline).
		var i int
		for n := 0; n < runesDisplayed && i < len(span.Content); n++ {
			_, sz := utf8.DecodeRuneInString(span.Content[i:])
			i += sz
		}
		firstTruncatedRune, _ := utf8.DecodeRuneInString(span.Content[i:])
		if firstTruncatedRune == '\n' {
			endedWithNewline = true
			runesDisplayed++
		} else if runesDisplayed == 0 && t.WrapPolicy == WrapWords {
			// The next word doesn't fit on the line. After other text, it
			// moves to the next line. On a line of its own, it is wider
			// than any line, so it breaks between graphemes (like CSS's
			// overflow-wrap: break-word). Upstream laid the rest of the
			// paragraph out here, untruncated: its lines all painted, but
			// counted as one, so the text overflowed below its box.
			if !lineEmpty {
				return spanResults{call: call, width: maxWidth + 1, runes: 0, multiLine: true}
			}
			call, ti = t.iterateSpan(gtx, maxWidth, span, true, text.WrapGraphemes)
			if ti.runes == 0 {
				// Not even one grapheme fits: show it anyway, overflowing.
				call, ti = t.iterateSpan(gtx, 1<<20, span, true, text.WrapGraphemes)
			}
			runesDisplayed = ti.runes
			multiLine = runesDisplayed < utf8.RuneCountInString(span.Content)
			endedWithNewline = ti.hasNewline
			if multiLine {
				r, _ := utf8.DecodeRuneInString(span.Content[byteOffset(span.Content, runesDisplayed):])
				if r == '\n' {
					endedWithNewline = true
					runesDisplayed++
				}
			}
		}
	}
	return spanResults{
		call:             call,
		carets:           ti.carets,
		width:            ti.bounds.Dx(),
		height:           ti.bounds.Dy(),
		ascent:           ti.baseline,
		runes:            runesDisplayed,
		multiLine:        multiLine,
		endedWithNewline: endedWithNewline,
	}
}

// Layout renders the TextStyle.
//
// The spanFn function, if not nil, gets called for each span after it has been
// drawn, with the offset set to the span's top left corner. This can be used to
// set up input handling, for example.
//
// The context's maximum constraint is set to the span's dimensions, while the
// dims argument additionally provides the text's baseline. The idx argument is
// the span's index in TextStyle.Styles. The function may get called multiple
// times with the same index if a span has to be broken across multiple lines.
func (t TextStyle) Layout(gtx layout.Context, spanFn func(gtx layout.Context, idx int, dims layout.Dimensions)) layout.Dimensions {
	spans := make([]SpanStyle, len(t.Styles))
	copy(spans, t.Styles)
	for i := range spans {
		spans[i].idx = i
	}

	// Compute the effective line height following the same logic as
	// text.Shaper.layoutParagraph: use LineHeight if set, otherwise
	// fall back to the largest span size, then scale by LineHeightScale
	// (defaulting to 1.2 if zero).
	lineHeightScale := t.LineHeightScale
	lineHeightPx := gtx.Sp(t.LineHeight)
	if lineHeightScale == 0 {
		lineHeightScale = 1.2
	}

	var (
		lineDims       image.Point
		lineAscent     int
		overallSize    image.Point
		lineShapes     []spanShape
		lineStartIndex int
		lineCarets     []Caret
	)
	if t.Carets != nil {
		n := 0
		for i := range spans {
			spans[i].runeOff = n
			n += utf8.RuneCountInString(spans[i].Content)
		}
	}

	for i := 0; i < len(spans); i++ {
		// grab the next span
		span := spans[i]

		// constrain the width of the line to the remaining space
		maxWidth := gtx.Constraints.Max.X - lineDims.X

		res := t.layoutSpan(gtx, maxWidth, span, lineDims.X == 0)

		// forceToNextLine handles the case in which the first segment of the new span does not fit
		// AND there is already content on the current line. If there is no content on the line,
		// we should display the content that doesn't fit anyway, as it won't fit on the next
		// line either.
		forceToNextLine := lineDims.X > 0 && res.width > maxWidth

		if !forceToNextLine {
			for _, c := range res.carets {
				c.X += lineDims.X
				c.Rune += span.runeOff
				lineCarets = append(lineCarets, c)
			}
			// store the text shaping results for the line
			lineShapes = append(lineShapes, spanShape{
				offset: image.Point{X: lineDims.X},
				size:   image.Point{X: res.width, Y: res.height},
				call:   res.call,
				ascent: res.ascent,
			})
			// update the dimensions of the current line
			lineDims.X += res.width
			if lineDims.Y < res.height {
				lineDims.Y = res.height
			}
			if lineAscent < res.ascent {
				lineAscent = res.ascent
			}

			// update the width of the overall text
			if overallSize.X < lineDims.X {
				overallSize.X = lineDims.X
			}

		}

		// if we are breaking the current span across lines or we are on the
		// last span, lay out all of the spans for the line.
		if res.multiLine || res.endedWithNewline || i == len(spans)-1 || forceToNextLine {
			lineMacro := op.Record(gtx.Ops)
			for i, shape := range lineShapes {
				// lay out this span
				span = spans[i+lineStartIndex]
				// Align the spans' baselines, not their tops, so a span
				// in another font (monospace) doesn't sit higher.
				shape.offset.Y = overallSize.Y + lineAscent - shape.ascent
				span.Layout(gtx, shape)

				if spanFn == nil {
					continue
				}
				offStack := op.Offset(shape.offset).Push(gtx.Ops)
				fnGtx := gtx
				fnGtx.Constraints.Min = image.Point{}
				fnGtx.Constraints.Max = shape.size
				spanFn(fnGtx, span.idx, layout.Dimensions{Size: shape.size, Baseline: shape.ascent})
				offStack.Pop()
			}
			lineCall := lineMacro.Stop()

			// Compute padding to align line. If the line is longer than can be displayed then padding is implicitly
			// limited to zero.
			finalShape := lineShapes[len(lineShapes)-1]
			lineWidth := finalShape.offset.X + finalShape.size.X
			var pad int
			if lineWidth < gtx.Constraints.Max.X {
				switch t.Alignment {
				case text.Start:
					pad = 0
				case text.Middle:
					pad = (gtx.Constraints.Max.X - lineWidth) / 2
				case text.End:
					pad = gtx.Constraints.Max.X - lineWidth
				}
			}

			stack := op.Offset(image.Pt(pad, 0)).Push(gtx.Ops)
			lineCall.Add(gtx.Ops)
			stack.Pop()

			// reset line shaping data and update overall vertical dimensions
			lineShapes = lineShapes[:0]
			// When we have lineHeight set, use it as the line height.
			// Otherwise, use the largest span size, then scale by LineHeightScale.
			effectiveLineHeight := lineDims.Y
			if t.LineHeight != 0 {
				effectiveLineHeight = lineHeightPx
			}
			effectiveLineHeight = int(float32(effectiveLineHeight) * lineHeightScale)
			if t.Carets != nil {
				for _, c := range lineCarets {
					c.X += pad
					c.Top, c.Bottom = overallSize.Y, overallSize.Y+effectiveLineHeight
					*t.Carets = append(*t.Carets, c)
				}
				lineCarets = lineCarets[:0]
			}
			overallSize.Y += effectiveLineHeight
			lineDims = image.Point{}
			lineAscent = 0
		}

		// if the current span breaks across lines
		if res.multiLine && !forceToNextLine {
			// mark where the next line to be laid out starts
			lineStartIndex = i + 1

			// ensure the spans slice has room for another span
			spans = append(spans, SpanStyle{})
			// shift existing spans further
			for k := len(spans) - 1; k > i+1; k-- {
				spans[k] = spans[k-1]
			}
			// synthesize and insert a new span
			byteLen := 0
			for i := 0; i < res.runes; i++ {
				_, n := utf8.DecodeRuneInString(span.Content[byteLen:])
				byteLen += n
			}
			span.Content = span.Content[byteLen:]
			span.runeOff += res.runes
			spans[i+1] = span
		} else if forceToNextLine {
			// mark where the next line to be laid out starts
			lineStartIndex = i
			i--
		} else if res.endedWithNewline {
			// mark where the next line to be laid out starts
			lineStartIndex = i + 1
		}
	}

	return layout.Dimensions{Size: gtx.Constraints.Constrain(overallSize)}
}

// byteOffset returns the byte offset of the n-th rune of s.
func byteOffset(s string, n int) int {
	i := 0
	for ; n > 0 && i < len(s); n-- {
		_, sz := utf8.DecodeRuneInString(s[i:])
		i += sz
	}
	return i
}
