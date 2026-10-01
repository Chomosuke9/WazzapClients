package photo

import (
	"image"
	"image/color"
	"math"
)

// Shrink scales src down to w x h, averaging the source pixels under each
// destination pixel (an area-weighted box filter, which is what downscaling
// a photo wants). It reads the source a row at a time and needs only a few
// rows of memory besides the result. x/image/draw's CatmullRom kernel, used
// before, allocates w x (source height) x 32 bytes: 157 MB to fit a
// 12-megapixel photo to a screen, which the Go heap then kept.
func Shrink(src image.Image, w, h int) *image.RGBA {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xs, ys := boxTaps(sw, w), boxTaps(sh, h)
	row := make([]byte, 4*sw)
	hrow := make([]float32, 4*w)
	acc := make([]float32, 4*w)
	last := -1 // source row in hrow
	for y := range h {
		clear(acc)
		for _, t := range ys.of(y) {
			if t.i != last {
				readRow(src, b.Min.Y+t.i, row)
				reduceRow(row, xs, hrow)
				last = t.i
			}
			for k, v := range hrow {
				acc[k] += t.w * v
			}
		}
		out := dst.Pix[y*dst.Stride : y*dst.Stride+4*w]
		for k, v := range acc {
			out[k] = uint8(min(255, v+0.5))
		}
	}
	return dst
}

// tap is a source pixel's share of a destination pixel.
type tap struct {
	i int
	w float32
}

// taps lists, for each destination pixel along one axis, the source pixels
// it covers and their weights, which add up to 1.
type taps struct {
	all   []tap
	start []int // taps of pixel j: all[start[j]:start[j+1]]
}

func (t taps) of(j int) []tap { return t.all[t.start[j]:t.start[j+1]] }

func boxTaps(n, m int) taps {
	scale := float64(n) / float64(m)
	t := taps{all: make([]tap, 0, n+m), start: make([]int, m+1)}
	for j := range m {
		lo, hi := float64(j)*scale, float64(j+1)*scale
		for i := int(lo); i < min(n, int(math.Ceil(hi))); i++ {
			if f := min(hi, float64(i+1)) - max(lo, float64(i)); f > 1e-9 {
				t.all = append(t.all, tap{i, float32(f / scale)})
			}
		}
		t.start[j+1] = len(t.all)
	}
	return t
}

// reduceRow averages a row of premultiplied RGBA bytes into out, one
// destination pixel per 4 floats.
func reduceRow(row []byte, xs taps, out []float32) {
	for x := range len(out) / 4 {
		var r, g, b, a float32
		for _, t := range xs.of(x) {
			p := row[4*t.i : 4*t.i+4]
			r += t.w * float32(p[0])
			g += t.w * float32(p[1])
			b += t.w * float32(p[2])
			a += t.w * float32(p[3])
		}
		out[4*x], out[4*x+1], out[4*x+2], out[4*x+3] = r, g, b, a
	}
}

// readRow reads source row y as premultiplied RGBA bytes.
func readRow(src image.Image, y int, row []byte) {
	b := src.Bounds()
	n := len(row) / 4
	switch s := src.(type) {
	case *image.YCbCr: // JPEG
		for x := range n {
			yi, ci := s.YOffset(b.Min.X+x, y), s.COffset(b.Min.X+x, y)
			row[4*x], row[4*x+1], row[4*x+2] = color.YCbCrToRGB(s.Y[yi], s.Cb[ci], s.Cr[ci])
			row[4*x+3] = 0xff
		}
	case *image.RGBA:
		copy(row, s.Pix[s.PixOffset(b.Min.X, y):])
	case *image.NRGBA: // PNG, WebP with alpha
		p := s.Pix[s.PixOffset(b.Min.X, y):]
		for x := range n {
			a := uint16(p[4*x+3])
			row[4*x] = uint8(uint16(p[4*x]) * a / 0xff)
			row[4*x+1] = uint8(uint16(p[4*x+1]) * a / 0xff)
			row[4*x+2] = uint8(uint16(p[4*x+2]) * a / 0xff)
			row[4*x+3] = uint8(a)
		}
	case *image.Gray:
		p := s.Pix[s.PixOffset(b.Min.X, y):]
		for x := range n {
			row[4*x], row[4*x+1], row[4*x+2], row[4*x+3] = p[x], p[x], p[x], 0xff
		}
	default:
		for x := range n {
			c := color.RGBAModel.Convert(src.At(b.Min.X+x, y)).(color.RGBA)
			row[4*x], row[4*x+1], row[4*x+2], row[4*x+3] = c.R, c.G, c.B, c.A
		}
	}
}
