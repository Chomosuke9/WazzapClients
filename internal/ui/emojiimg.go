package ui

import (
	"image"
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
// place from the embedded font, or nil when ch isn't a single color glyph.
// It is safe to call from any goroutine.
func emojiPNG(ch string) []byte {
	emojiFontOnce.Do(func() {
		bundledFonts() // patches the emoji font's spaces first
		lds, err := ot.NewLoaders(ot.NewBytesReader(notoColorEmoji))
		if err != nil || len(lds) == 0 {
			return
		}
		emojiFont, _ = fontapi.NewFont(lds[0])
	})
	if emojiFont == nil {
		return nil
	}
	face := fontapi.NewFace(emojiFont)
	text := []rune(ch)
	var sh shaping.HarfbuzzShaper
	out := sh.Shape(shaping.Input{Text: text, RunEnd: len(text), Face: face,
		Size: fixed.I(notoPpem), Script: language.Common, Language: language.NewLanguage("en")})
	if len(out.Glyphs) != 1 {
		return nil
	}
	bm, ok := face.GlyphDataBitmap(out.Glyphs[0].GlyphID)
	if !ok || bm.Format != fontapi.PNG {
		return nil
	}
	return bm.Data
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
