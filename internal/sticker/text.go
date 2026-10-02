package sticker

import (
	_ "embed"
	"image"
	"image/color"
	"math"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Text is meme text drawn along the top and bottom of a sticker's picture.
type Text struct {
	Top, Bottom string
}

// Anton is an Impact-like face under the SIL Open Font License
// (fonts/Anton-OFL.txt), the same as WazzapAgent's stickers use. It's
// parsed for each sticker rather than kept: stickers are rare.
//
//go:embed fonts/Anton-Regular.ttf
var antonTTF []byte

// drawText draws the top and bottom text inside area, the part of dst the
// picture covers, in capitals: white letters with a black outline.
func drawText(dst *image.RGBA, text Text, area image.Rectangle) error {
	top := strings.ToUpper(strings.TrimSpace(text.Top))
	bottom := strings.ToUpper(strings.TrimSpace(text.Bottom))
	if top == "" && bottom == "" {
		return nil
	}
	f, err := opentype.Parse(antonTTF)
	if err != nil {
		return err
	}
	pad := max(4, min(area.Dx(), area.Dy())/25)
	// Each block takes at most 40% of the height (a lone one 80%), so the
	// two never overlap.
	share := 0.4
	if top == "" || bottom == "" {
		share = 0.8
	}
	maxH := int(float64(area.Dy()-2*pad) * share)
	maxW := area.Dx() - 2*pad
	for _, b := range []struct {
		text string
		top  bool
	}{{top, true}, {bottom, false}} {
		if b.text == "" {
			continue
		}
		face, lines, lineH, err := fitText(f, b.text, maxW, maxH)
		if err != nil {
			return err
		}
		y := area.Min.Y + pad
		if !b.top {
			y = area.Max.Y - pad - lineH*len(lines)
		}
		for _, l := range lines {
			drawOutlined(dst, face, l, area.Min.X+area.Dx()/2, y+lineH)
			y += lineH
		}
		face.Close()
	}
	return nil
}

// fitText picks the largest size, from about a tenth of the sticker down,
// at which the words wrap into lines that fit maxW by maxH.
func fitText(f *opentype.Font, text string, maxW, maxH int) (font.Face, []string, int, error) {
	const largest, smallest = 64, 16
	for size := largest; ; size -= 2 {
		face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			return nil, nil, 0, err
		}
		lines, fits := wrapText(face, text, maxW)
		lineH := int(math.Ceil(float64(size) * 1.1))
		if fits && lineH*len(lines) <= maxH || size <= smallest {
			return face, lines, lineH, nil
		}
		face.Close()
	}
}

// wrapText breaks text into lines no wider than maxW. fits is false when
// a word alone is wider.
func wrapText(face font.Face, text string, maxW int) (lines []string, fits bool) {
	limit := fixed.I(maxW)
	fits = true
	cur := ""
	for _, w := range strings.Fields(text) {
		if font.MeasureString(face, w) > limit {
			fits = false
		}
		next := w
		if cur != "" {
			next = cur + " " + w
		}
		if cur != "" && font.MeasureString(face, next) > limit {
			lines = append(lines, cur)
			cur = w
			continue
		}
		cur = next
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines, fits
}

// drawOutlined draws line centered on cx with its bottom at bottom: a
// black outline of offset copies, then the white letters.
func drawOutlined(dst *image.RGBA, face font.Face, line string, cx, bottom int) {
	w := font.MeasureString(face, line)
	origin := fixed.Point26_6{X: fixed.I(cx) - w/2, Y: fixed.I(bottom - face.Metrics().Descent.Ceil())}
	r := max(2, face.Metrics().Height.Ceil()/14)
	d := &font.Drawer{Dst: dst, Src: image.NewUniform(color.Black), Face: face}
	for a := 0.0; a < 2*math.Pi; a += math.Pi / 8 {
		d.Dot = origin.Add(fixed.P(int(math.Round(float64(r)*math.Cos(a))), int(math.Round(float64(r)*math.Sin(a)))))
		d.DrawString(line)
	}
	d.Src = image.NewUniform(color.White)
	d.Dot = origin
	d.DrawString(line)
}
