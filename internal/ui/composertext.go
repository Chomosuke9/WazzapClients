package ui

import (
	"image"
	"strings"
	"time"
	"unicode/utf8"

	"gioui.org/f32"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/widget/material"
	"golang.org/x/image/math/fixed"
)

// The composer shows WhatsApp formatting as you type: *bold*, _italic_,
// ~strike~ and code in their style, with the marks faded. widget.Editor
// paints all its text in one color and one font, so it paints none (its
// text is transparent) and paintComposerText draws the text, glyph for
// glyph where the editor placed it. Bold and italic must keep the regular
// glyphs' advances, or the caret and selection would no longer match the
// text: bold is the outline painted twice a fraction of a pixel apart, and
// italic the regular glyphs slanted.

// Per-byte flags of the composer text, beside the textStyle bits.
const (
	cMark    = 1 << 6 // a formatting mark
	cMention = 1 << 7 // a picked @mention
)

// composerRich reports whether the composer text needs paintComposerText.
func (u *UI) composerRich(txt string) bool {
	return strings.ContainsAny(txt, "*_~`") || len(u.mentionRanges(txt)) > 0
}

// composerFlags returns the style of each byte of txt, cached for the last
// text.
func (u *UI) composerFlags(txt string) []uint8 {
	c := &u.conv
	if c.richFor == txt && c.richFlags != nil {
		return c.richFlags
	}
	fl := make([]uint8, len(txt))
	parts := strings.Split(txt, "```")
	off := 0
	for i, p := range parts {
		if i > 0 {
			// The fence before part i opens a block (closed unless it's the
			// last) or closes one.
			if i%2 == 0 || i < len(parts)-1 {
				for j := off - 3; j < off; j++ {
					fl[j] = cMark
				}
			}
		}
		switch {
		case i%2 == 1 && i < len(parts)-1:
			for j := off; j < off+len(p); j++ {
				fl[j] = uint8(styleMono)
			}
		case i%2 == 1: // an unterminated block: the fence is text
			markInline(txt[off-3:off+len(p)], off-3, 0, fl)
		default:
			markInline(p, off, 0, fl)
		}
		off += len(p) + 3
	}
	// Mentions, from rune to byte offsets.
	if rs := u.mentionRanges(txt); len(rs) > 0 {
		ri := 0
		for bi := range txt {
			for _, r := range rs {
				if ri >= r[0] && ri < r[1] {
					_, n := utf8.DecodeRuneInString(txt[bi:])
					for j := bi; j < bi+n; j++ {
						fl[j] |= cMention
					}
				}
			}
			ri++
		}
	}
	c.richFor, c.richFlags = txt, fl
	return fl
}

// markInline flags the marks and styled text of s, at offset base of the
// whole text, the way parseInline parses it.
func markInline(s string, base int, style textStyle, fl []uint8) {
	fill := func(a, b int, st textStyle) {
		for j := a; j < b; j++ {
			fl[base+j] = uint8(st)
		}
	}
	start := 0
	for i := 0; i < len(s); i++ {
		st, ok := markerStyle[s[i]]
		if !ok || style&st != 0 {
			continue
		}
		if i > 0 {
			if r, _ := utf8.DecodeLastRuneInString(s[:i]); isWordRune(r) {
				continue
			}
		}
		if i+1 >= len(s) || s[i+1] == ' ' || s[i+1] == '\n' {
			continue
		}
		j := closingMarker(s, i)
		if j < 0 {
			continue
		}
		fill(start, i, style)
		fl[base+i] = cMark
		if st == styleCode {
			fill(i+1, j, style|st)
		} else {
			markInline(s[i+1:j], base+i+1, style|st, fl)
		}
		fl[base+j] = cMark
		start = j + 1
		i = j
	}
	fill(start, len(s), style)
}

// paintComposerText draws the composer's text with its formatting, and
// its caret, over the editor laid out with e in gtx, of size sz.
func (u *UI) paintComposerText(gtx C, e material.EditorStyle, txt string, sz image.Point) {
	c := &u.conv
	ed := e.Editor
	fl := u.composerFlags(txt)
	sh := u.th.Shaper
	em := float32(gtx.Sp(e.TextSize))
	sh.LayoutString(text.Parameters{
		Font:     e.Font,
		PxPerEm:  fixed.I(gtx.Sp(e.TextSize)),
		MaxWidth: gtx.Constraints.Max.X,
		MinWidth: gtx.Constraints.Min.X,
		Locale:   gtx.Locale,
	}, txt)
	// Shape it all first: the editor may be scrolled, which shows in
	// where it puts the caret compared to where the caret's rune is here.
	caret, _ := ed.Selection()
	glyphs := c.richGlyphs[:0]
	caretAt := image.Point{-1, -1}
	ri := 0
	for {
		g, ok := sh.NextGlyph()
		if !ok {
			break
		}
		if ri >= caret && caretAt.Y < 0 {
			caretAt = image.Pt(g.X.Round(), int(g.Y))
		}
		glyphs = append(glyphs, g)
		if g.Flags&text.FlagClusterBreak != 0 {
			ri += int(g.Runes)
		}
	}
	c.richGlyphs = glyphs
	if len(glyphs) == 0 {
		return
	}
	viewY := ed.CaretCoords().Round().Y
	if caretAt.Y < 0 { // after the last rune
		g := glyphs[len(glyphs)-1]
		caretAt = image.Pt((g.X + g.Advance).Round(), int(g.Y))
		if strings.HasSuffix(txt, "\n") {
			// The caret is on the empty line after it: compare the
			// line break itself instead.
			if rs := ed.Regions(ri-1, ri, c.richRegions[:0]); len(rs) > 0 {
				c.richRegions = rs
				viewY = rs[0].Bounds.Max.Y - rs[0].Baseline
			}
		}
	}
	scroll := caretAt.Y - viewY
	asc, desc := glyphs[0].Ascent.Ceil(), glyphs[0].Descent.Ceil()
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()

	run := c.richRun[:0]
	var runFl uint8
	flush := func() {
		if len(run) > 0 {
			u.paintComposerRun(gtx, run, runFl, scroll, em)
		}
		run = run[:0]
	}
	bi := 0 // byte offset of the cluster's first rune
	cs := 0 // index of the cluster's first glyph
	for i, g := range glyphs {
		if g.Flags&text.FlagClusterBreak == 0 {
			continue
		}
		f := uint8(0)
		if bi < len(fl) {
			f = fl[bi]
		}
		if len(run) > 0 && (f != runFl || run[0].Y != g.Y) {
			flush()
		}
		runFl = f
		run = append(run, glyphs[cs:i+1]...)
		cs = i + 1
		for range g.Runes {
			if bi < len(txt) {
				_, n := utf8.DecodeRuneInString(txt[bi:])
				bi += n
			}
		}
	}
	flush()
	c.richRun = run

	u.paintComposerCaret(gtx, asc, desc)
}

// paintComposerRun draws glyphs of one line that share their flags f.
func (u *UI) paintComposerRun(gtx C, run []text.Glyph, f uint8, scroll int, em float32) {
	p := u.pal
	sh := u.th.Shaper
	g0 := run[0]
	last := run[len(run)-1]
	x0 := g0.X.Floor()
	base := int(g0.Y) - scroll
	w := (last.X + last.Advance).Ceil() - x0
	col := p.Text
	switch {
	case f&cMark != 0:
		col = p.ComposerHint
	case f&cMention != 0:
		col = p.Green
	}
	st := textStyle(f &^ (cMark | cMention))
	if f&cMark != 0 {
		st = 0
	}
	if st&(styleCode|styleMono) != 0 {
		r := image.Rect(x0, base-int(em*0.98+0.5), x0+w, base+int(em*0.26+0.5))
		fillRRect(gtx, r, gtx.Dp(4), p.CodeBg)
	}
	path := sh.Shape(run)
	draw := func(dx float32) {
		tr := f32.AffineId().Offset(f32.Pt(float32(x0)+dx, float32(base)))
		if st&styleItalic != 0 {
			// Slant around the baseline: x moves right as y goes up.
			tr = f32.AffineId().Shear(f32.Point{}, -0.2, 0).Offset(f32.Pt(float32(x0)+dx, float32(base)))
		}
		t := op.Affine(tr).Push(gtx.Ops)
		cl := clip.Outline{Path: path}.Op().Push(gtx.Ops)
		paint.ColorOp{Color: col}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		cl.Pop()
		if dx == 0 {
			if call := sh.Bitmaps(run); call != (op.CallOp{}) {
				call.Add(gtx.Ops)
			}
		}
		t.Pop()
	}
	draw(0)
	if st&styleBold != 0 {
		draw(max(0.7, em*0.045))
	}
	if st&styleStrike != 0 {
		y := base - int(em*0.3+0.5)
		fillRect(gtx, image.Rect(x0, y, x0+w, y+max(1, gtx.Dp(1))), col)
	}
}

// paintComposerCaret draws the caret the editor's transparent text color
// hides, blinking as the editor's would: steadily for 10 s after it moves.
func (u *UI) paintComposerCaret(gtx C, asc, desc int) {
	c := &u.conv
	ed := &c.composer
	if !gtx.Focused(ed) {
		c.caretFocused = false
		return
	}
	a, b := ed.Selection()
	key := [3]int{a, b, ed.Len()}
	if key != c.caretKey || !c.caretFocused {
		c.caretKey, c.caretSince = key, gtx.Now
	}
	c.caretFocused = true
	const blink = time.Second
	dt := gtx.Now.Sub(c.caretSince)
	if dt < 10*time.Second {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(blink/2 - dt%(blink/2))})
	}
	if dt < 10*time.Second && dt%blink >= blink/2 {
		return
	}
	pt := ed.CaretCoords().Round()
	hw := max(gtx.Dp(1)/2, 1)
	fillRect(gtx, image.Rect(pt.X-hw, pt.Y-asc, pt.X+hw, pt.Y+desc), u.pal.Text)
}
