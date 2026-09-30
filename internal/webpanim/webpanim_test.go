package webpanim

import (
	"encoding/binary"
	"image"
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
