//go:build windows

package osclip

import (
	"encoding/binary"
	"image/color"
	"testing"
)

// dib builds a packed DIB of w x h pixels, px(x, y) giving each one's
// BGR(A) bytes.
func dib(w, h, bpp int, comp uint32, topDown bool, px func(x, y int) []byte) []byte {
	stride := (w*bpp + 31) / 32 * 4
	hdr := 40
	if comp == biBitfields {
		hdr += 12
	}
	b := make([]byte, hdr+stride*h)
	le := binary.LittleEndian
	le.PutUint32(b, 40)
	le.PutUint32(b[4:], uint32(w))
	hh := int32(h)
	if topDown {
		hh = -hh
	}
	le.PutUint32(b[8:], uint32(hh))
	le.PutUint16(b[12:], 1)
	le.PutUint16(b[14:], uint16(bpp))
	le.PutUint32(b[16:], comp)
	if comp == biBitfields {
		le.PutUint32(b[40:], 0xff0000)
		le.PutUint32(b[44:], 0xff00)
		le.PutUint32(b[48:], 0xff)
	}
	for y := range h {
		row := y
		if !topDown {
			row = h - 1 - y
		}
		for x := range w {
			copy(b[hdr+row*stride+x*bpp/8:], px(x, y))
		}
	}
	return b
}

func TestParseDIB(t *testing.T) {
	red, blue := []byte{0, 0, 0xff, 0}, []byte{0xff, 0, 0, 0}
	px := func(x, y int) []byte {
		if y == 0 {
			return red
		}
		return blue
	}
	for _, tc := range []struct {
		name    string
		bpp     int
		comp    uint32
		topDown bool
	}{
		{"24-bit bottom-up", 24, biRGB, false},
		{"32-bit bottom-up", 32, biRGB, false},
		{"32-bit bitfields top-down", 32, biBitfields, true},
	} {
		img := parseDIB(dib(3, 2, tc.bpp, tc.comp, tc.topDown, func(x, y int) []byte { return px(x, y)[:tc.bpp/8] }))
		if img == nil {
			t.Fatalf("%s: not parsed", tc.name)
		}
		if got := color.NRGBAModel.Convert(img.At(2, 0)); got != (color.NRGBA{R: 0xff, A: 0xff}) {
			t.Errorf("%s: the top row is %v, want opaque red", tc.name, got)
		}
		if got := color.NRGBAModel.Convert(img.At(0, 1)); got != (color.NRGBA{B: 0xff, A: 0xff}) {
			t.Errorf("%s: the bottom row is %v, want opaque blue", tc.name, got)
		}
	}
	if parseDIB([]byte{1, 2, 3}) != nil {
		t.Error("a short buffer parsed")
	}
}
