// Package sticker makes WhatsApp stickers: a picture fitted into a 512x512
// transparent canvas, with meme text if you like, as a lossless WebP, or
// a video, GIF or animated WebP as an animated one (anim.go). It
// has its own WebP encoder (vp8l.go), since golang.org/x/image only
// decodes WebP, and libwebp needs cgo or, translated to Go
// (github.com/gen2brain/webp), adds megabytes to the executable and
// registers a second WebP decoder for image.Decode.
package sticker

import (
	"bytes"
	"errors"
	"image"
	"image/draw"

	_ "image/gif" // the formats a sticker can be made from
	_ "image/jpeg"
	_ "image/png"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/chomosuke9/wazzapclients/internal/photo"
)

// Size is the width and height of a sticker.
const Size = 512

// MaxBytes is the largest sticker this makes; WhatsApp may not show
// bigger ones.
const MaxBytes = 1 << 20

// maxPixels bounds the picture to decode (96 MB as RGBA).
const maxPixels = 24 << 20

// ErrTooLarge means the picture is too big to decode.
var ErrTooLarge = errors.New("sticker: the picture is too large")

// ErrAnimated means the picture is an animated WebP, which FromImage
// can't make a sticker of (Make can).
var ErrAnimated = errors.New("sticker: the picture is animated")

// FromImage makes a sticker of a JPEG, PNG, GIF (its first frame) or still
// WebP picture, with text drawn on it.
func FromImage(data []byte, text Text) ([]byte, error) {
	if animatedWebP(data) {
		return nil, ErrAnimated
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return nil, ErrTooLarge
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	c, area := canvas(src)
	if err := drawText(c, text, area); err != nil {
		return nil, err
	}
	// A photo may not fit losslessly: encodeStill drops low bits of its
	// colors until it does.
	return encodeStill(c)
}

// animatedWebP reports whether data is a WebP with animation.
func animatedWebP(data []byte) bool {
	// RIFF, size, WEBP, then a VP8X chunk whose flags say so.
	return len(data) >= 21 && string(data[0:4]) == "RIFF" && string(data[8:16]) == "WEBPVP8X" && data[20]&0x02 != 0
}

// canvas fits img into the middle of a transparent Size x Size canvas, and
// returns it with the area the picture covers.
func canvas(img image.Image) (*image.RGBA, image.Rectangle) {
	c := image.NewRGBA(image.Rect(0, 0, Size, Size))
	return c, fit(c, img)
}

// fit draws img into the middle of c, a Size x Size canvas, clearing the
// rest, and returns the area it covers.
func fit(c *image.RGBA, img image.Image) image.Rectangle {
	clear(c.Pix)
	b := img.Bounds()
	w, h := Size, Size
	if b.Dx() > b.Dy() {
		h = max(1, Size*b.Dy()/b.Dx())
	} else {
		w = max(1, Size*b.Dx()/b.Dy())
	}
	area := image.Rect(0, 0, w, h).Add(image.Pt((Size-w)/2, (Size-h)/2))
	switch {
	case b.Dx() == w && b.Dy() == h:
		draw.Draw(c, area, img, b.Min, draw.Src)
	case b.Dx() >= w && b.Dy() >= h:
		draw.Draw(c, area, photo.Shrink(img, w, h), image.Point{}, draw.Src)
	default:
		// Kernel scalers allocate w x (source height) x 32 bytes;
		// ApproxBiLinear allocates nothing.
		xdraw.ApproxBiLinear.Scale(c, area, img, b, draw.Src, nil)
	}
	return area
}

// unpremultiply returns the pixels of img as non-premultiplied ARGB, as
// WebP keeps them.
func unpremultiply(img *image.RGBA) []uint32 {
	b := img.Bounds()
	out := make([]uint32, 0, b.Dx()*b.Dy())
	for y := range b.Dy() {
		row := img.Pix[y*img.Stride : y*img.Stride+4*b.Dx()]
		for x := range b.Dx() {
			p := row[4*x : 4*x+4]
			r, g, bl, a := uint32(p[0]), uint32(p[1]), uint32(p[2]), uint32(p[3])
			if a != 0 && a != 0xff {
				r, g, bl = min(255, (r*255+a/2)/a), min(255, (g*255+a/2)/a), min(255, (bl*255+a/2)/a)
			}
			out = append(out, a<<24|r<<16|g<<8|bl)
		}
	}
	return out
}
