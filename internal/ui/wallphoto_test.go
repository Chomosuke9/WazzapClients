package ui

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/chomosuke9/wazzapclients/internal/mock"
)

// TestPhotoWallValue checks a photo wallpaper's focus survives the prefs,
// and that one kept before there was a focus is centered.
func TestPhotoWallValue(t *testing.T) {
	v := photoWallValue("ab.jpg", f32.Pt(0.25, 0.8))
	if v != "photo:ab.jpg@250,800" {
		t.Errorf("value = %q", v)
	}
	if file, f := parsePhotoWall(v[len("photo:"):]); file != "ab.jpg" || f != f32.Pt(0.25, 0.8) {
		t.Errorf("parsed %q %v", file, f)
	}
	if file, f := parsePhotoWall("ab.jpg"); file != "ab.jpg" || f != wallFocusCenter {
		t.Errorf("old value parsed %q %v", file, f)
	}
}

// TestWallView checks the part of a photo a pane shows keeps the focus in
// view, in the middle unless that shows past the photo's edge.
func TestWallView(t *testing.T) {
	for _, c := range []struct {
		name         string
		src, pane    image.Point
		focus        f32.Point
		wantX, wantY float32
	}{
		// A portrait photo in a wide pane: its full width, cut at the top.
		{"portrait top", image.Pt(1440, 2560), image.Pt(1100, 800), f32.Pt(0.5, 0.1), 0, 0},
		{"portrait middle", image.Pt(1440, 2560), image.Pt(1100, 800), f32.Pt(0.5, 0.5), 0, (2560 - 1440*800/1100.0) / 2},
		// A landscape photo in a tall pane: a third of the width, at the left.
		{"landscape left", image.Pt(2560, 1440), image.Pt(500, 1000), f32.Pt(0.2, 0.5), 2560*0.2 - 720/2, 0},
	} {
		v, s := wallView(c.src, c.pane, c.focus)
		if d := v.x - c.wantX; d > 0.5 || d < -0.5 {
			t.Errorf("%s: x = %v, want %v", c.name, v.x, c.wantX)
		}
		if d := v.y - c.wantY; d > 0.5 || d < -0.5 {
			t.Errorf("%s: y = %v, want %v", c.name, v.y, c.wantY)
		}
		if w, h := v.w*s, v.h*s; w < float32(c.pane.X)-0.5 || h < float32(c.pane.Y)-0.5 {
			t.Errorf("%s: shows %vx%v of a %v pane", c.name, w, h, c.pane)
		}
	}
}

// TestWallPhotoPart checks a photo wallpaper decodes only the part the
// pane shows, at the pane's size, and decodes it again, sharp, when the
// window grows.
func TestWallPhotoPart(t *testing.T) {
	dir := t.TempDir()
	old := wallpaperDir
	wallpaperDir = dir
	defer func() { wallpaperDir = old }()
	img := image.NewRGBA(image.Rect(0, 0, 1440, 2560))
	for i := range img.Pix {
		img.Pix[i] = uint8(i)
	}
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	f, err := os.Create(filepath.Join(dir, "p.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	jpeg.Encode(f, img, nil)
	f.Close()

	u := New(mock.New())
	u.Start(func() {})
	path := filepath.Join(dir, "p.jpg")
	focus := f32.Pt(0.5, 0.4)
	now := testNow()
	draw := func(pane image.Point) photoWall {
		t.Helper()
		var ops op.Ops
		deadline := time.Now().Add(10 * time.Second)
		for {
			ops.Reset()
			gtx := layout.Context{Ops: &ops, Now: now, Constraints: layout.Exact(pane)}
			u.layoutWallPhoto(gtx, path, focus)
			u.images.endFrame()
			w := u.themeUI.wall
			if w.key != "" && w.size == w.wantSize && w.crop == w.want {
				return w
			}
			if time.Now().After(deadline) {
				t.Fatalf("pane %v: part %v at %v never came (want %v at %v)", pane, w.crop, w.size, w.want, w.wantSize)
			}
			now = now.Add(50 * time.Millisecond)
			time.Sleep(20 * time.Millisecond)
		}
	}
	small := draw(image.Pt(550, 400))
	big := draw(image.Pt(1100, 800))
	for _, w := range []photoWall{small, big} {
		// The photo's full width, a band of it around the focus.
		if w.crop.Dx() != 1440 || w.crop.Min.Y <= 0 || w.crop.Max.Y >= 2560 {
			t.Errorf("crop = %v", w.crop)
		}
		if mid := (w.crop.Min.Y + w.crop.Max.Y) / 2; mid < 1024-5 || mid > 1024+5 {
			t.Errorf("crop %v isn't around the focus", w.crop)
		}
	}
	if small.size.X != 550 || big.size.X != 1100 {
		t.Errorf("parts decoded at %v and %v, want the panes' widths", small.size, big.size)
	}
	u.images.mu.Lock()
	_, kept := u.images.m[small.key]
	u.images.mu.Unlock()
	if kept {
		t.Error("the small part is still cached after the big one came")
	}
}
