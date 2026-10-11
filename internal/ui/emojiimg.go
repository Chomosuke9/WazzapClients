package ui

import (
	"image"
	"slices"
	"strings"
	"sync"

	"gioui.org/op"
	fontapi "github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

// The emoji picker draws its emojis as pictures scaled to the cell, not as
// text. Gio's shaper keeps up to 1000 color glyphs decoded at the font's
// full 136x128 size (70 KB each) and never lets them go, so scrolling
// through the picker once pinned about 70 MB. These come from a small
// cache of their own, bounded by bytes.

// The bundled emoji font has one bitmap strike: 109 pixels per em, with
// pictures 136 pixels wide (and 128 high).
const (
	notoPpem  = 109
	notoWidth = 136
)

var (
	emojiFontOnce sync.Once
	emojiFont     *fontapi.Font
)

// emojiPNG returns the PNG of ch's glyph in the bundled emoji font, read in
// place from the embedded font (or the 3D one, see emojiBitmap), or nil
// when ch isn't a single color glyph.
// It is safe to call from any goroutine.
func emojiPNG(ch string) []byte {
	face, glyphs := shapeEmoji([]rune(ch))
	if len(glyphs) != 1 {
		return nil
	}
	bm, ok := face.GlyphDataBitmap(glyphs[0].GlyphID)
	if !ok || bm.Format != fontapi.PNG {
		return nil
	}
	return bm.Data
}

// loadEmojiFont parses the bundled emoji font, once, for shapeEmoji. Its
// pictures follow the emoji style (emojiBitmap), as the shaper's do.
func loadEmojiFont() *fontapi.Font {
	emojiFontOnce.Do(func() {
		bundledFonts() // patches the emoji font's spaces first
		lds, err := ot.NewLoaders(ot.NewBytesReader(notoColorEmoji))
		if err != nil || len(lds) == 0 {
			return
		}
		if f, err := fontapi.NewFont(lds[0]); err == nil {
			f.BitmapHook = emojiBitmap
			emojiFont = f
		}
	})
	return emojiFont
}

// shapeEmoji shapes text with the bundled emoji font alone.
func shapeEmoji(text []rune) (*fontapi.Face, []shaping.Glyph) {
	if loadEmojiFont() == nil {
		return nil, nil
	}
	face := fontapi.NewFace(emojiFont)
	var sh shaping.HarfbuzzShaper
	out := sh.Shape(shaping.Input{Text: text, RunEnd: len(text), Face: face,
		Size: fixed.I(notoPpem), Script: language.Common, Language: language.NewLanguage("en")})
	return face, out.Glyphs
}

// jumboMax is how many emojis a message may hold and still show them big,
// without a bubble, as WhatsApp does.
const jumboMax = 3

var jumbos = memo[string, []string]{limit: 200}

// jumboEmojis returns the emojis of s when s is nothing but one to
// jumboMax of them (spaces aside), or nil.
func jumboEmojis(s string) []string {
	if s == "" || len(s) > 96 {
		return nil
	}
	return jumbos.get(s, func() []string { return splitEmojis(s) })
}

// splitEmojis splits s into the emojis the bundled font draws as one
// color picture each, or returns nil when s holds anything else.
func splitEmojis(s string) []string {
	text := []rune(strings.Join(strings.Fields(s), ""))
	if len(text) == 0 {
		return nil
	}
	face, glyphs := shapeEmoji(text)
	if len(glyphs) == 0 || len(glyphs) > jumboMax {
		return nil
	}
	var out []string
	for i, g := range glyphs {
		end := len(text)
		if i+1 < len(glyphs) {
			end = glyphs[i+1].ClusterIndex
		}
		if g.GlyphID == 0 || g.ClusterIndex >= end {
			return nil
		}
		if bm, ok := face.GlyphDataBitmap(g.GlyphID); !ok || bm.Format != fontapi.PNG {
			return nil
		}
		cl := text[g.ClusterIndex:end]
		// The font has pictures of digits, # and * for keycaps; alone
		// they are text.
		if !slices.ContainsFunc(cl, func(r rune) bool { return r >= 0x80 }) {
			return nil
		}
		out = append(out, string(cl))
	}
	return out
}

// layoutEmojiImage draws ch at text size px (like a label of that size
// would), centered in a box of size. It reports false when ch has no
// picture to draw, and the caller should fall back to a label.
func (u *UI) layoutEmojiImage(gtx C, ch string, px int, size image.Point) bool {
	// Gio draws a bitmap glyph scaled by px/ppem.
	side := px * notoWidth / notoPpem
	e := u.emojiImgs.get(ch, side, func() []byte { return emojiPNG(ch) })
	if e.state == imgMissing {
		return false
	}
	if e.state != imgReady {
		return true // loading: leave the cell blank for a frame or two
	}
	w, h := e.size.X, e.size.Y
	if s := max(w, h); s != side && s > 0 {
		w, h = w*side/s, h*side/s
	}
	defer op.Offset(image.Pt((size.X-w)/2, (size.Y-h)/2)).Push(gtx.Ops).Pop()
	paintCover(gtx, e.op, e.size, image.Rect(0, 0, w, h))
	return true
}
