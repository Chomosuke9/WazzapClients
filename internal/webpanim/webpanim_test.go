package webpanim

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"testing"
	"time"
)

func le24(v int) []byte { return []byte{byte(v), byte(v >> 8), byte(v >> 16)} }

func chunk(id string, p []byte) []byte {
	b := append([]byte(id), binary.LittleEndian.AppendUint32(nil, uint32(len(p)))...)
	b = append(b, p...)
	if len(p)&1 != 0 {
		b = append(b, 0)
	}
	return b
}

func anmf(x, y, w, h, ms int, flags byte, frame []byte) []byte {
	var p []byte
	p = append(p, le24(x/2)...)
	p = append(p, le24(y/2)...)
	p = append(p, le24(w-1)...)
	p = append(p, le24(h-1)...)
	p = append(p, le24(ms)...)
	p = append(p, flags)
	return chunk("ANMF", append(p, frame...))
}

func TestParse(t *testing.T) {
	vp8x := append([]byte{0x12, 0, 0, 0}, append(le24(511), le24(255)...)...)
	body := []byte("WEBP")
	body = append(body, chunk("VP8X", vp8x)...)
	body = append(body, chunk("ANIM", []byte{0, 0, 0, 0, 3, 0})...)
	// An odd-sized payload checks the padding; the bitstreams are dummies.
	body = append(body, anmf(10, 20, 100, 50, 80, 0x02,
		append(chunk("ALPH", []byte{1, 2, 3}), chunk("VP8 ", []byte{4, 5})...))...)
	body = append(body, anmf(0, 0, 512, 256, 0, 0x01, chunk("VP8L", []byte{6}))...)
	data := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(len(body)))...)
	data = append(data, body...)

	if !IsAnimated(data) {
		t.Fatal("not recognized as animated")
	}
	a, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if a.Width != 512 || a.Height != 256 || a.Loops != 3 || len(a.Frames) != 2 {
		t.Fatalf("got %dx%d, %d loops, %d frames", a.Width, a.Height, a.Loops, len(a.Frames))
	}
	f := a.Frames[0]
	if f.Rect != image.Rect(10, 20, 110, 70) || f.Delay != 80*time.Millisecond || f.blend || f.dispose ||
		string(f.alph) != "\x01\x02\x03" || string(f.vp8) != "\x04\x05" || f.lossless {
		t.Fatalf("frame 0: %+v", f)
	}
	f = a.Frames[1]
	if f.Delay != 100*time.Millisecond || !f.blend || !f.dispose || !f.lossless {
		t.Fatalf("frame 1: %+v", f)
	}
}

// TestReadYCbCr: the 4:2:0 fast path reads what color.YCbCrToRGB does,
// from any starting pixel, odd or even, of a picture that doesn't start
// at 0,0.
func TestReadYCbCr(t *testing.T) {
	m := image.NewYCbCr(image.Rect(3, 5, 40, 30), image.YCbCrSubsampleRatio420)
	seed := uint32(1)
	for _, p := range [][]byte{m.Y, m.Cb, m.Cr} {
		for i := range p {
			seed = seed*1664525 + 1013904223
			p[i] = byte(seed >> 24)
		}
	}
	for y := m.Rect.Min.Y; y < m.Rect.Max.Y; y++ {
		for x := m.Rect.Min.X; x < m.Rect.Max.X; x++ {
			row := make([]byte, 4*(m.Rect.Max.X-x))
			readYCbCr(row, m, nil, x, y)
			for i := 0; i < len(row)/4; i++ {
				c := m.YCbCrAt(x+i, y)
				r, g, b := color.YCbCrToRGB(c.Y, c.Cb, c.Cr)
				if row[4*i] != r || row[4*i+1] != g || row[4*i+2] != b || row[4*i+3] != 0xff {
					t.Fatalf("from %d,%d, pixel %d is %v, want %v", x, y, i, row[4*i:4*i+4], []byte{r, g, b, 0xff})
				}
			}
		}
	}
	// With alpha, transparent pixels are black and the others keep their
	// color.
	ma := &image.NYCbCrA{YCbCr: *m, A: make([]byte, len(m.Y)), AStride: m.YStride}
	for i := range ma.A {
		ma.A[i] = byte(i % 3 * 127)
	}
	b := m.Rect
	row := make([]byte, 4*b.Dx())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		readRow(row, ma, b.Min.X, y)
		for i := 0; i < b.Dx(); i++ {
			c := ma.NYCbCrAAt(b.Min.X+i, y)
			want := []byte{0, 0, 0, 0}
			if c.A != 0 {
				r, g, bl := color.YCbCrToRGB(c.Y, c.Cb, c.Cr)
				want = []byte{r, g, bl, c.A}
			}
			if !bytes.Equal(row[4*i:4*i+4], want) {
				t.Fatalf("pixel %d,%d is %v, want %v", b.Min.X+i, y, row[4*i:4*i+4], want)
			}
		}
	}
}
