package photo

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestSquare(t *testing.T) {
	// A wide picture, red in the middle third and blue at the sides.
	src := image.NewRGBA(image.Rect(0, 0, 1500, 500))
	for y := range 500 {
		for x := range 1500 {
			c := color.RGBA{0, 0, 255, 255}
			if x >= 500 && x < 1000 {
				c = color.RGBA{255, 0, 0, 255}
			}
			src.Set(x, y, c)
		}
	}
	var in bytes.Buffer
	if err := png.Encode(&in, src); err != nil {
		t.Fatal(err)
	}
	out, err := Square(in.Bytes(), 200)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 200 || b.Dy() != 200 {
		t.Fatalf("size %v, want 200x200", b.Size())
	}
	for _, p := range []image.Point{{2, 2}, {197, 197}, {100, 100}} {
		r, g, b, _ := img.At(p.X, p.Y).RGBA()
		if r>>8 < 200 || g>>8 > 60 || b>>8 > 60 {
			t.Errorf("pixel %v is %d,%d,%d, want red (the middle square)", p, r>>8, g>>8, b>>8)
		}
	}
	// Small pictures aren't scaled up.
	small, _ := Square(in.Bytes(), 1000)
	if img, _ := jpeg.Decode(bytes.NewReader(small)); img.Bounds().Dx() != 500 {
		t.Errorf("a 500 px square came out %d px", img.Bounds().Dx())
	}
}
