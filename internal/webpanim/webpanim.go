// Package webpanim decodes animated WebP images (animated stickers).
//
// golang.org/x/image/webp only reads still images. This package splits an
// animated file into its frames, hands each frame's bitstream to x/image/webp,
// and composites the result onto a canvas, one frame at a time. Only the
// canvas stays decoded, never the whole animation.
package webpanim

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"time"

	"golang.org/x/image/webp"
)

var errFormat = errors.New("webpanim: invalid format")

// Frame is one frame of an animation.
type Frame struct {
	Rect     image.Rectangle // where it goes on the canvas
	Delay    time.Duration   // how long it shows
	blend    bool            // alpha-blend onto the canvas; else replace
	dispose  bool            // clear its rectangle before the next frame
	alph     []byte          // ALPH chunk payload, for VP8 frames with alpha
	vp8      []byte          // VP8 or VP8L chunk payload
	lossless bool
}

// Anim is a parsed animation. Next renders it frame by frame.
type Anim struct {
	Width, Height int
	Loops         int // 0 means forever
	Frames        []Frame

	next   int
	canvas *image.RGBA
	buf    []byte // scratch: the single-frame container
	row    []byte // scratch: one decoded row
}

// IsAnimated reports whether data is an animated WebP file.
func IsAnimated(data []byte) bool {
	if len(data) < 30 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" || string(data[12:16]) != "VP8X" {
		return false
	}
	return data[20]&0x02 != 0
}

// Parse reads an animated WebP file. data is kept, not copied.
func Parse(data []byte) (*Anim, error) {
	if !IsAnimated(data) {
		return nil, errFormat
	}
	a := &Anim{}
	err := chunks(data[12:], func(id string, p []byte) error {
		switch id {
		case "VP8X":
			if len(p) < 10 {
				return errFormat
			}
			a.Width, a.Height = u24(p[4:])+1, u24(p[7:])+1
		case "ANIM":
			if len(p) < 6 {
				return errFormat
			}
			a.Loops = int(binary.LittleEndian.Uint16(p[4:]))
		case "ANMF":
			f, err := parseFrame(p)
			if err != nil {
				return err
			}
			a.Frames = append(a.Frames, f)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if a.Width <= 0 || a.Height <= 0 || a.Width*a.Height > 4096*4096 || len(a.Frames) == 0 {
		return nil, errFormat
	}
	return a, nil
}

func parseFrame(p []byte) (Frame, error) {
	if len(p) < 16 {
		return Frame{}, errFormat
	}
	x, y := 2*u24(p), 2*u24(p[3:])
	w, h := u24(p[6:])+1, u24(p[9:])+1
	delay := time.Duration(u24(p[12:])) * time.Millisecond
	if delay <= 10*time.Millisecond {
		delay = 100 * time.Millisecond // what browsers do with 0 and tiny delays
	}
	f := Frame{
		Rect:    image.Rect(x, y, x+w, y+h),
		Delay:   delay,
		blend:   p[15]&0x02 == 0,
		dispose: p[15]&0x01 != 0,
	}
	err := chunks(p[16:], func(id string, c []byte) error {
		switch id {
		case "ALPH":
			f.alph = c
		case "VP8 ":
			f.vp8 = c
		case "VP8L":
			f.vp8, f.lossless = c, true
		}
		return nil
	})
	if err == nil && f.vp8 == nil {
		err = errFormat
	}
	return f, err
}

// chunks calls fn for each RIFF chunk in b.
func chunks(b []byte, fn func(id string, payload []byte) error) error {
	for len(b) >= 8 {
		id := string(b[:4])
		n := int(binary.LittleEndian.Uint32(b[4:]))
		if n < 0 || n > len(b)-8 {
			return errFormat
		}
		if err := fn(id, b[8:8+n]); err != nil {
			return err
		}
		n += n & 1 // chunks are padded to even sizes
		if 8+n > len(b) {
			break
		}
		b = b[8+n:]
	}
	return nil
}

func u24(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 }

// Next draws the next frame onto the canvas and returns the canvas and how
// long to show it. The canvas is reused: it changes on the next call. After
// the last frame it starts over.
func (a *Anim) Next() (canvas *image.RGBA, delay time.Duration, err error) {
	if a.canvas == nil {
		a.canvas = image.NewRGBA(image.Rect(0, 0, a.Width, a.Height))
	}
	if a.next == 0 {
		clear(a.canvas.Pix)
	} else if prev := a.Frames[a.next-1]; prev.dispose {
		clearRect(a.canvas, prev.Rect)
	}
	f := a.Frames[a.next]
	a.next = (a.next + 1) % len(a.Frames)
	img, err := a.decode(f)
	if err != nil {
		return nil, 0, err
	}
	a.composite(f.Rect.Min, img, f.blend)
	return a.canvas, f.Delay, nil
}

// Rewind makes the next call to Next draw the first frame.
func (a *Anim) Rewind() { a.next = 0 }

// decode wraps one frame's chunks into a still WebP file for x/image/webp.
func (a *Anim) decode(f Frame) (image.Image, error) {
	b := a.buf[:0]
	b = append(b, "RIFF\x00\x00\x00\x00WEBP"...)
	if f.alph != nil && !f.lossless {
		w, h := f.Rect.Dx()-1, f.Rect.Dy()-1
		b = appendChunk(b, "VP8X", []byte{0x10, 0, 0, 0,
			byte(w), byte(w >> 8), byte(w >> 16), byte(h), byte(h >> 8), byte(h >> 16)})
		b = appendChunk(b, "ALPH", f.alph)
	}
	if f.lossless {
		b = appendChunk(b, "VP8L", f.vp8)
	} else {
		b = appendChunk(b, "VP8 ", f.vp8)
	}
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	a.buf = b
	return webp.Decode(bytes.NewReader(b))
}

func appendChunk(b []byte, id string, p []byte) []byte {
	b = append(b, id...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(p)))
	b = append(b, p...)
	if len(p)&1 != 0 {
		b = append(b, 0)
	}
	return b
}

func clearRect(dst *image.RGBA, r image.Rectangle) {
	r = r.Intersect(dst.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		i := dst.PixOffset(r.Min.X, y)
		clear(dst.Pix[i : i+4*r.Dx()])
	}
}

// composite draws src at off, premultiplying its alpha, either blended over
// what's there or replacing it.
func (a *Anim) composite(off image.Point, src image.Image, blend bool) {
	dst := a.canvas
	b := src.Bounds()
	r := b.Sub(b.Min).Add(off).Intersect(dst.Rect)
	if r.Empty() {
		return
	}
	if cap(a.row) < 4*r.Dx() {
		a.row = make([]byte, 4*r.Dx())
	}
	row := a.row[:4*r.Dx()]
	for y := r.Min.Y; y < r.Max.Y; y++ {
		sy := y - off.Y + b.Min.Y
		sx0 := r.Min.X - off.X + b.Min.X
		readRow(row, src, sx0, sy)
		d := dst.Pix[dst.PixOffset(r.Min.X, y):]
		for i := 0; i < len(row); i += 4 {
			cr, cg, cb, ca := uint32(row[i]), uint32(row[i+1]), uint32(row[i+2]), uint32(row[i+3])
			if ca != 0xff {
				cr, cg, cb = (cr*ca+127)/255, (cg*ca+127)/255, (cb*ca+127)/255
				if blend {
					k := 255 - ca
					cr += (uint32(d[i])*k + 127) / 255
					cg += (uint32(d[i+1])*k + 127) / 255
					cb += (uint32(d[i+2])*k + 127) / 255
					ca += (uint32(d[i+3])*k + 127) / 255
				}
			}
			d[i], d[i+1], d[i+2], d[i+3] = uint8(cr), uint8(cg), uint8(cb), uint8(ca)
		}
	}
}

// readRow reads len(row)/4 pixels of src starting at x, y as non-premultiplied RGBA.
func readRow(row []byte, src image.Image, x, y int) {
	n := len(row) / 4
	switch s := src.(type) {
	case *image.NYCbCrA:
		readYCbCr(row, &s.YCbCr, x, y)
		a := s.A[s.AOffset(x, y):]
		for i := 0; i < n; i++ {
			row[4*i+3] = a[i]
		}
	case *image.YCbCr:
		readYCbCr(row, s, x, y)
	case *image.NRGBA:
		copy(row, s.Pix[s.PixOffset(x, y):])
	default:
		for i := 0; i < n; i++ {
			c := color.NRGBAModel.Convert(src.At(x+i, y)).(color.NRGBA)
			row[4*i], row[4*i+1], row[4*i+2], row[4*i+3] = c.R, c.G, c.B, c.A
		}
	}
}

func readYCbCr(row []byte, s *image.YCbCr, x, y int) {
	for i := 0; i < len(row)/4; i++ {
		yi, ci := s.YOffset(x+i, y), s.COffset(x+i, y)
		row[4*i], row[4*i+1], row[4*i+2] = color.YCbCrToRGB(s.Y[yi], s.Cb[ci], s.Cr[ci])
		row[4*i+3] = 0xff
	}
}
