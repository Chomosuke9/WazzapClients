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

// TestWallView checks the part of a photo a pane shows has the focus in the
// middle, whichever side is cut, and stays in the photo.
func TestWallView(t *testing.T) {
	portrait, landscape := image.Pt(1440, 2560), image.Pt(2560, 1440)
	for _, src := range []image.Point{portrait, landscape} {
		for _, pane := range []image.Point{{X: 1100, Y: 800}, {X: 500, Y: 1000}, {X: 2000, Y: 400}} {
			for _, f := range []f32.Point{wallFocusCenter, {X: 0.637, Y: 0.546}, {X: 0.2, Y: 0.5}, {X: 0.5, Y: 0.13}, {X: 0.85, Y: 0.8}, {}, {X: 1, Y: 1}} {
				v, s := wallView(src, pane, f)
				mid := f32.Pt((v.x+v.w/2)/float32(src.X), (v.y+v.h/2)/float32(src.Y))
				near := func(a, b float32) bool { return a-b < 0.001 && b-a < 0.001 }
				// At the photo's edge the middle is as near as the zoom lets it.
				want := f32.Pt(min(max(f.X, wallMinPart/2), 1-wallMinPart/2), min(max(f.Y, wallMinPart/2), 1-wallMinPart/2))
				if !near(mid.X, want.X) || !near(mid.Y, want.Y) {
					t.Errorf("%v in %v around %v: the middle is %v, want %v", src, pane, f, mid, want)
				}
				if v.x < -0.5 || v.y < -0.5 || v.x+v.w > float32(src.X)+0.5 || v.y+v.h > float32(src.Y)+0.5 {
					t.Errorf("%v in %v around %v: shows %+v, past the photo", src, pane, f, v)
				}
				if w, h := v.w*s, v.h*s; w < float32(pane.X)-0.5 || h < float32(pane.Y)-0.5 {
					t.Errorf("%v in %v around %v: shows %vx%v", src, pane, f, w, h)
				}
			}
		}
	}
	// In the middle the photo isn't zoomed in to: its full width.
	if v, _ := wallView(portrait, image.Pt(1100, 800), wallFocusCenter); v.w != 1440 {
		t.Errorf("around its middle %v of the photo's width shows", v.w)
	}
}

// TestSharedCrop checks a photo is set for everyone cropped so the focus is
// its middle.
func TestSharedCrop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.jpg")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	jpeg.Encode(f, image.NewRGBA(image.Rect(0, 0, 200, 400)), nil)
	f.Close()
	if c := sharedCrop(path, wallFocusCenter); !c.Empty() {
		t.Errorf("a photo without a point picked is cropped to %v", c)
	}
	if c, want := sharedCrop(path, f32.Pt(0.6, 0.25)), image.Rect(40, 0, 200, 200); c != want {
		t.Errorf("cropped to %v, want %v", c, want)
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
