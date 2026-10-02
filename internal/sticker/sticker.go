// Package sticker makes WhatsApp stickers: a picture fitted into a 512x512
// transparent canvas, as a lossless WebP. It has its own WebP encoder
// (vp8l.go), since golang.org/x/image only decodes WebP and the libwebp
// bindings need cgo or keep a WebAssembly runtime in memory.
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

// FromImage makes a sticker of a JPEG, PNG, GIF (its first frame) or still
// WebP picture.
func FromImage(data []byte) ([]byte, error) {
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
	argb := canvas(src)
	// A photo may not fit losslessly: drop low bits of its colors until
	// it does.
	var out []byte
	for quant := range uint(4) {
		out = encodeWebP(argb, Size, Size, quant)
		if len(out) <= MaxBytes {
			return out, nil
		}
	}
	return nil, errors.New("sticker: the picture doesn't fit in a sticker")
}

// canvas fits img into the middle of a transparent Size x Size canvas and
// returns its pixels as non-premultiplied ARGB.
func canvas(img image.Image) []uint32 {
	b := img.Bounds()
	w, h := Size, Size
	if b.Dx() > b.Dy() {
		h = max(1, Size*b.Dy()/b.Dx())
	} else {
		w = max(1, Size*b.Dx()/b.Dy())
	}
	var fit *image.RGBA
	if b.Dx() >= w && b.Dy() >= h {
		fit = photo.Shrink(img, w, h)
	} else {
		// Kernel scalers allocate w x (source height) x 32 bytes;
		// ApproxBiLinear allocates nothing.
		fit = image.NewRGBA(image.Rect(0, 0, w, h))
		xdraw.ApproxBiLinear.Scale(fit, fit.Bounds(), img, b, draw.Src, nil)
	}
	out := make([]uint32, Size*Size)
	x0, y0 := (Size-w)/2, (Size-h)/2
	for y := range h {
		row := fit.Pix[y*fit.Stride : y*fit.Stride+4*w]
		for x := range w {
			p := row[4*x : 4*x+4]
			r, g, bl, a := uint32(p[0]), uint32(p[1]), uint32(p[2]), uint32(p[3])
			if a != 0 && a != 0xff {
				// image.RGBA is premultiplied; WebP isn't.
				r, g, bl = min(255, (r*255+a/2)/a), min(255, (g*255+a/2)/a), min(255, (bl*255+a/2)/a)
			}
			out[(y0+y)*Size+x0+x] = a<<24 | r<<16 | g<<8 | bl
		}
	}
	return out
}
