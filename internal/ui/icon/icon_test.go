package icon

import (
	"image"
	"image/draw"
	"testing"

	"golang.org/x/image/vector"
)

func TestParseAndRasterize(t *testing.T) {
	// Package init already parsed every generated icon; check that one
	// renders ink inside its bounds.
	z := vector.NewRasterizer(48, 48)
	Chat.Rasterize(z, 0, 0, 48, 0)
	dst := image.NewAlpha(image.Rect(0, 0, 48, 48))
	z.Draw(dst, dst.Bounds(), image.Opaque, image.Point{})
	ink := 0
	for _, a := range dst.Pix {
		if a > 128 {
			ink++
		}
	}
	if ink < 100 || ink > 48*48/2 {
		t.Fatalf("unexpected ink coverage %d", ink)
	}
	_ = draw.Over
}

func TestRelativeAndShorthand(t *testing.T) {
	ic, err := Parse("M0 0h10v10H0Z m2 2 l1 1 q1 1 2 2 t2 2", 0, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(ic.segs); got != 9 {
		t.Fatalf("got %d segments", got)
	}
	// After Z, "m2 2" is relative to the subpath start (0,0).
	if m := ic.segs[5]; m.op != 'M' || m.a.X != 0.2 || m.a.Y != 0.2 {
		t.Fatalf("relative move after close: %+v", m)
	}
}
