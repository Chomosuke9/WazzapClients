package photo

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

func TestPrepare(t *testing.T) {
	// A wide PNG, half transparent.
	src := image.NewNRGBA(image.Rect(0, 0, 3200, 1800))
	for y := range 1800 {
		for x := range 1600 {
			src.SetNRGBA(x, y, color.NRGBA{0, 0, 0xff, 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()

	for _, c := range []struct {
		q    model.Quality
		w, h int
		png  bool
	}{
		{model.QualityStandard, 1600, 900, false},
		{model.QualityHD, 3200, 1800, false},
		{model.QualityRaw, 3200, 1800, true},
	} {
		p, err := Prepare(data, c.q)
		if err != nil {
			t.Fatal(err)
		}
		if p.W != c.w || p.H != c.h || p.PNG != c.png {
			t.Errorf("quality %d: %dx%d png=%v, want %dx%d png=%v", c.q, p.W, p.H, p.PNG, c.w, c.h, c.png)
		}
		if c.png {
			if !bytes.Equal(p.Data, data) {
				t.Errorf("raw: the file changed")
			}
			continue
		}
		img, err := jpeg.Decode(bytes.NewReader(p.Data))
		if err != nil {
			t.Fatalf("quality %d: %v", c.q, err)
		}
		if b := img.Bounds(); b.Dx() != c.w || b.Dy() != c.h {
			t.Errorf("quality %d: JPEG is %v", c.q, b)
		}
		// The transparent half turns white, not black.
		if r, g, b, _ := img.At(c.w*3/4, c.h/2).RGBA(); r>>8 < 0xf0 || g>>8 < 0xf0 || b>>8 < 0xf0 {
			t.Errorf("quality %d: transparent part is %x %x %x", c.q, r>>8, g>>8, b>>8)
		}
		if len(p.Thumb) == 0 {
			t.Errorf("quality %d: no thumbnail", c.q)
		}
	}

	e, err := Estimates(data)
	if err != nil {
		t.Fatal(err)
	}
	if e.W[model.QualityStandard] != 1600 || e.Bytes[model.QualityRaw] != len(data) {
		t.Errorf("estimates: %+v", e)
	}
}
