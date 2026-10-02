package sticker

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"testing"

	"golang.org/x/image/webp"
)

// decodeARGB decodes a WebP with x/image and returns its pixels as ARGB.
func decodeARGB(t *testing.T, data []byte) ([]uint32, int, int) {
	t.Helper()
	img, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	n, ok := img.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded a %T, want *image.NRGBA", img)
	}
	b := n.Bounds()
	out := make([]uint32, 0, b.Dx()*b.Dy())
	for y := range b.Dy() {
		for x := range b.Dx() {
			c := n.NRGBAAt(x, y)
			out = append(out, uint32(c.A)<<24|uint32(c.R)<<16|uint32(c.G)<<8|uint32(c.B))
		}
	}
	return out, b.Dx(), b.Dy()
}

// testImages are pixel patterns that exercise the encoder's paths: flat
// areas (simple codes, long copies), gradients (predictors) and noise
// (big codes, length-limited Huffman trees).
func testImages(w, h int) map[string][]uint32 {
	rng := rand.New(rand.NewPCG(1, 2))
	imgs := map[string][]uint32{}
	flat := make([]uint32, w*h)
	for i := range flat {
		flat[i] = 0xff336699
	}
	imgs["flat"] = flat
	grad := make([]uint32, w*h)
	for y := range h {
		for x := range w {
			grad[y*w+x] = 0xff000000 | uint32(x*255/max(1, w-1))<<16 | uint32(y*255/max(1, h-1))<<8 | uint32((x+y)&0xff)
		}
	}
	imgs["gradient"] = grad
	noise := make([]uint32, w*h)
	for i := range noise {
		noise[i] = rng.Uint32()
	}
	imgs["noise"] = noise
	mixed := make([]uint32, w*h)
	for y := range h {
		for x := range w {
			switch {
			case x < w/3:
				mixed[y*w+x] = 0 // transparent padding
			case y%7 == 0:
				mixed[y*w+x] = 0x80ff0000 | uint32(x&0xff)
			default:
				mixed[y*w+x] = 0xff000000 | rng.Uint32()&0x0f0f0f | uint32(y&0xff)<<8
			}
		}
	}
	imgs["mixed"] = mixed
	return imgs
}

func TestEncodeLossless(t *testing.T) {
	for _, sz := range [][2]int{{1, 1}, {3, 2}, {17, 33}, {64, 64}, {130, 70}} {
		for name, pix := range testImages(sz[0], sz[1]) {
			data := encodeWebP(pix, sz[0], sz[1], 0)
			got, w, h := decodeARGB(t, data)
			if w != sz[0] || h != sz[1] {
				t.Fatalf("%s %v: decoded %dx%d", name, sz, w, h)
			}
			for i, c := range pix {
				if c>>24 == 0 {
					c = 0
				}
				if got[i] != c {
					t.Fatalf("%s %v: pixel %d is %08x, want %08x", name, sz, i, got[i], c)
				}
			}
		}
	}
}

func TestEncodeQuantized(t *testing.T) {
	const w, h = 40, 30
	for name, pix := range testImages(w, h) {
		for quant := uint(1); quant < 4; quant++ {
			got, _, _ := decodeARGB(t, encodeWebP(pix, w, h, quant))
			mask := 0xff000000 | uint32(0xff>>quant<<quant)*0x010101
			for i, c := range pix {
				if c>>24 == 0 {
					c = 0
				}
				if want := c & mask; got[i] != want {
					t.Fatalf("%s quant %d: pixel %d is %08x, want %08x", name, quant, i, got[i], want)
				}
			}
		}
	}
}

func TestPrefixCode(t *testing.T) {
	// The decoder's lz77Param, inverted.
	param := func(sym int, extra uint32) int {
		if sym < 4 {
			return sym + 1
		}
		bits := (sym - 2) >> 1
		return (2+sym&1)<<bits + int(extra) + 1
	}
	for v := 1; v <= 1<<20; v += 1 + v/7 {
		sym, nbits, extra := prefixCode(v)
		if sym >= numDists || extra >= 1<<nbits && nbits > 0 {
			t.Fatalf("prefixCode(%d) = %d, %d, %d", v, sym, nbits, extra)
		}
		if got := param(sym, extra); got != v {
			t.Fatalf("prefixCode(%d) decodes to %d", v, got)
		}
	}
}

func TestFromImage(t *testing.T) {
	for _, sz := range [][2]int{{800, 600}, {120, 300}, {512, 512}} {
		src := image.NewNRGBA(image.Rect(0, 0, sz[0], sz[1]))
		rng := rand.New(rand.NewPCG(3, 4))
		for y := range sz[1] {
			for x := range sz[0] {
				src.SetNRGBA(x, y, color.NRGBA{uint8(x), uint8(y), uint8(rng.IntN(40)), 0xff})
			}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, src); err != nil {
			t.Fatal(err)
		}
		data, err := FromImage(buf.Bytes())
		if err != nil {
			t.Fatalf("%v: %v", sz, err)
		}
		if len(data) > MaxBytes {
			t.Fatalf("%v: %d bytes", sz, len(data))
		}
		got, w, h := decodeARGB(t, data)
		if w != Size || h != Size {
			t.Fatalf("%v: sticker is %dx%d", sz, w, h)
		}
		// The picture is centered on a transparent canvas.
		if sz[0] != sz[1] && got[0] != 0 {
			t.Errorf("%v: corner is %08x, want transparent", sz, got[0])
		}
		if c := got[Size/2*Size+Size/2]; c>>24 != 0xff {
			t.Errorf("%v: middle is %08x, want opaque", sz, c)
		}
	}
}

// A noisy photo-sized picture must still fit.
func TestFromImageNoise(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
	rng := rand.New(rand.NewPCG(5, 6))
	for i := range src.Pix {
		src.Pix[i] = uint8(rng.Uint32())
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	data, err := FromImage(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > MaxBytes {
		t.Fatalf("%d bytes", len(data))
	}
	decodeARGB(t, data)
}
