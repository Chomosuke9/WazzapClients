package photo

import (
	"image"
	"image/color"
	"testing"
)

func TestShrink(t *testing.T) {
	// Vertical stripes one pixel wide average to grey; the halves stay apart.
	src := image.NewRGBA(image.Rect(0, 0, 90, 60))
	for y := range 60 {
		for x := range 90 {
			c := color.RGBA{0, 0, 0, 0xff}
			if x%2 == 0 {
				c = color.RGBA{0xff, 0xff, 0xff, 0xff}
			}
			if y >= 30 {
				c = color.RGBA{0, 0, 0xff, 0xff}
			}
			src.SetRGBA(x, y, c)
		}
	}
	dst := Shrink(src, 30, 20)
	if got := dst.Bounds().Size(); got != image.Pt(30, 20) {
		t.Fatalf("size %v", got)
	}
	// Each destination pixel covers three columns: two white and one
	// black, or the other way around.
	if c := dst.RGBAAt(0, 0); c.R < 0xa9 || c.R > 0xab || c.A != 0xff {
		t.Errorf("stripes averaged to %v, want 2/3 white", c)
	}
	if c := dst.RGBAAt(1, 0); c.R < 0x54 || c.R > 0x56 {
		t.Errorf("stripes averaged to %v, want 1/3 white", c)
	}
	if c := dst.RGBAAt(5, 15); c != (color.RGBA{0, 0, 0xff, 0xff}) {
		t.Errorf("solid half is %v", c)
	}

	// Non-integer ratios keep solid colors exact, and transparent pixels
	// don't darken their neighbors (NRGBA is premultiplied first).
	n := image.NewNRGBA(image.Rect(10, 10, 107, 71))
	for y := 10; y < 71; y++ {
		for x := 10; x < 107; x++ {
			c := color.NRGBA{0xff, 0x80, 0, 0xff}
			if x%3 == 0 {
				c = color.NRGBA{0, 0, 0, 0}
			}
			n.SetNRGBA(x, y, c)
		}
	}
	d := Shrink(n, 13, 8)
	for y := range 8 {
		for x := range 13 {
			c := d.RGBAAt(x, y)
			// Unpremultiplied, the color is the opaque pixels' color.
			if c.A < 0x8c || c.A > 0xcc || int(c.R) != int(c.A) || absDiff(int(c.G)*0xff/int(c.A), 0x80) > 2 {
				t.Fatalf("pixel %d,%d = %v", x, y, c)
			}
		}
	}

	// JPEG's YCbCr with subsampled chroma.
	yc := image.NewYCbCr(image.Rect(0, 0, 64, 48), image.YCbCrSubsampleRatio420)
	for i := range yc.Y {
		yc.Y[i] = 0x80
	}
	for i := range yc.Cb {
		yc.Cb[i], yc.Cr[i] = 0x80, 0x80
	}
	if c := Shrink(yc, 20, 15).RGBAAt(7, 7); c != (color.RGBA{0x80, 0x80, 0x80, 0xff}) {
		t.Errorf("grey YCbCr came out %v", c)
	}
}

func absDiff(a, b int) int { return max(a-b, b-a) }
