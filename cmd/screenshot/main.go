// Command screenshot renders the UI off-screen and saves PNG previews.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/ui"
)

func main() {
	out := flag.String("out", "docs", "output directory")
	width := flag.Int("w", 1280, "width in dp")
	height := flag.Int("h", 800, "height in dp")
	scale := flag.Float64("scale", 1.5, "pixels per dp")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	shots := []struct {
		name string
		dark bool
		chat int
	}{
		{"preview-light.png", false, 0},
		{"preview-dark.png", true, 0},
		{"preview-group.png", false, 1},
		{"preview-empty.png", false, -1},
	}
	for _, s := range shots {
		u := ui.New()
		u.SetDark(s.dark)
		u.Select(s.chat)
		path := filepath.Join(*out, s.name)
		if err := render(u, path, *width, *height, float32(*scale)); err != nil {
			log.Fatalf("%s: %v", s.name, err)
		}
		fmt.Println("wrote", path)
	}
}

func render(u *ui.UI, path string, wDp, hDp int, scale float32) error {
	w, h := int(float32(wDp)*scale), int(float32(hDp)*scale)
	win, err := headless.NewWindow(w, h)
	if err != nil {
		return err
	}
	defer win.Release()

	var ops op.Ops
	// Lay out twice: lists settle their scroll position on the first pass.
	for i := 0; i < 2; i++ {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Now:         time.Now(),
			Metric:      unit.Metric{PxPerDp: scale, PxPerSp: scale},
			Constraints: layout.Exact(image.Pt(w, h)),
		}
		u.Layout(gtx)
	}
	if err := win.Frame(&ops); err != nil {
		return err
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := win.Screenshot(img); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
