package main

import (
	"fmt"
	"image"
	"image/draw"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	xdraw "golang.org/x/image/draw"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/ui"
)

// film renders an animation as a grid of frames, step apart: the top row
// opens the overlay, the bottom row closes it again with Esc. Besides the
// -overlay names, "info" opens the info panel, "message" receives a message
// and "reorder" moves a chat up the list (these two have no closing row),
// "hover" moves the pointer onto the -at point and then away, and "typing"
// shows someone typing, then their message replacing the bubble.
func film(name, chat string, x, y, w, h int, scale float32, step time.Duration) (*image.RGBA, error) {
	const frames = 6
	b := mock.New()
	u := ui.New(b)
	u.Start(func() {})
	u.SetDark(true)
	u.SelectID(chat)

	win, err := headless.NewWindow(w, h)
	if err != nil {
		return nil, err
	}
	defer win.Release()
	now := time.Now()
	var ops op.Ops
	var router input.Router
	frame := func(capture bool) (*image.RGBA, error) {
		ops.Reset()
		u.Layout(layout.Context{
			Ops:         &ops,
			Now:         now,
			Metric:      unit.Metric{PxPerDp: scale, PxPerSp: scale},
			Constraints: layout.Exact(image.Pt(w, h)),
			Source:      router.Source(),
		})
		router.Frame(&ops)
		if !capture {
			return nil, nil
		}
		if err := win.Frame(&ops); err != nil {
			return nil, err
		}
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		return img, win.Screenshot(img)
	}
	settle := func() {
		for range 4 {
			now = now.Add(time.Second)
			frame(false)
		}
	}
	row := func() ([]*image.RGBA, error) {
		var out []*image.RGBA
		for range frames {
			img, err := frame(true)
			if err != nil {
				return nil, err
			}
			out = append(out, img)
			now = now.Add(step)
		}
		return out, nil
	}

	point := func(x, y int) {
		router.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(x), float32(y)), Time: time.Duration(now.UnixNano())})
	}
	settle()
	switch name {
	case "hover":
		point(x, y)
	case "info":
		u.ShowInfo(0, 0)
	case "message":
		if ms := b.Messages(chat, 1); len(ms) > 0 {
			b.Forward(ms, []string{chat})
		}
	case "reorder":
		// A chat far down the list gets a message and moves up.
		if ms := b.Messages(chat, 1); len(ms) > 0 {
			b.Forward(ms, []string{"gym"})
		}
	case "typing":
		b.SetTyping(chat, "Clara", true)
	default:
		u.ShowOverlay(name, x, y)
	}
	open, err := row()
	if err != nil {
		return nil, err
	}
	rows := [][]*image.RGBA{open}
	if name != "message" && name != "reorder" {
		settle()
		switch name {
		case "hover":
			point(w-2, h-2)
		case "typing":
			// They stop typing and their message comes right after,
			// taking the bubble's place.
			b.SetTyping(chat, "", false)
			b.Receive(chat, "Clara", "Sure, sending it now")
		default:
			u.Escape()
		}
		shut, err := row()
		if err != nil {
			return nil, err
		}
		rows = append(rows, shut)
	}

	// Frames at half size, in a grid.
	fw, fh := w/2, h/2
	grid := image.NewRGBA(image.Rect(0, 0, fw*frames, fh*len(rows)))
	for r, imgs := range rows {
		for i, img := range imgs {
			dst := image.Rect(i*fw, r*fh, (i+1)*fw, (r+1)*fh)
			xdraw.ApproxBiLinear.Scale(grid, dst, img, img.Bounds(), draw.Src, nil)
		}
	}
	fmt.Printf("film: %d frames per row, %v apart\n", frames, step)
	return grid, nil
}
