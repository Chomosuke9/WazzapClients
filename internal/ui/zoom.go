package ui

import (
	"image"
	"math"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/op/clip"
)

const maxZoom = 5

// zoomPan is the zoom and pan of a picture in a viewer. The wheel zooms
// toward the pointer, keeping the spot under it in place; dragging pans
// while zoomed in. The picture never leaves a gap at an edge it could fill.
type zoomPan struct {
	zoom float32   // 1 fits the picture in the area
	pan  f32.Point // the picture's center, from the area's center
	drag struct {
		active, moved bool
		start, last   f32.Point
	}
	// What is shown glides to zoom and pan. Zoom and pan glide along the
	// same curve, so the spot under the pointer stays put on the way.
	shownZoom, panX, panY follower

	area image.Rectangle // last frame's, for input
	fit  f32.Point       // the picture's size at zoom 1, last frame
}

// reset goes back to the fitted picture.
func (z *zoomPan) reset() { z.zoom, z.pan = 1, f32.Point{} }

func (z *zoomPan) zoomed() bool { return z.zoom > 1 }

// zoomAt zooms to `to`, keeping the picture's point at p (in the area's
// coordinates) where it is.
func (z *zoomPan) zoomAt(p f32.Point, to float32) {
	if z.zoom <= 0 {
		z.zoom = 1
	}
	to = min(maxZoom, max(1, to))
	k := to / z.zoom
	c := pointF(z.area.Min.Add(z.area.Size().Div(2))).Add(z.pan)
	z.pan = z.pan.Add(p.Sub(c).Mul(1 - k))
	z.zoom = to
	z.clamp()
}

// zoomCenter zooms to `to` around the middle of the area (the zoom buttons).
func (z *zoomPan) zoomCenter(to float32) {
	z.zoomAt(pointF(z.area.Min.Add(z.area.Size().Div(2))), to)
}

// clamp keeps the zoomed picture covering the area where it can.
func (z *zoomPan) clamp() {
	lim := func(fit float32, area int) float32 {
		return max(0, (fit*z.zoom-float32(area))/2)
	}
	lx, ly := lim(z.fit.X, z.area.Dx()), lim(z.fit.Y, z.area.Dy())
	z.pan.X = min(lx, max(-lx, z.pan.X))
	z.pan.Y = min(ly, max(-ly, z.pan.Y))
}

// update handles the wheel and dragging. It reports a click (a press and
// release that didn't move) and whether the pointer moved over the area.
func (z *zoomPan) update(gtx C) (clicked, moved bool) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: z, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Scroll | pointer.Move,
			ScrollY: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20}})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Move:
			moved = true
		case pointer.Press:
			if e.Buttons.Contain(pointer.ButtonPrimary) {
				z.drag.active, z.drag.moved, z.drag.start, z.drag.last = true, false, e.Position, e.Position
			}
		case pointer.Drag:
			if !z.drag.active {
				continue
			}
			if d := e.Position.Sub(z.drag.start); d.X*d.X+d.Y*d.Y > float32(gtx.Dp(5)*gtx.Dp(5)) {
				z.drag.moved = true
			}
			if z.zoomed() {
				z.pan = z.pan.Add(e.Position.Sub(z.drag.last))
				z.clamp()
				z.panX.snap(z.pan.X) // dragging follows the pointer exactly
				z.panY.snap(z.pan.Y)
			}
			z.drag.last = e.Position
		case pointer.Release:
			clicked = clicked || z.drag.active && !z.drag.moved
			z.drag.active = false
		case pointer.Cancel:
			z.drag.active = false
		case pointer.Scroll:
			// 1.15× per wheel notch (120 on Windows); touchpads send
			// smaller steps.
			z.zoomAt(e.Position, z.zoom*float32(math.Pow(1.15, float64(-e.Scroll.Y)/120)))
		}
	}
	return clicked, moved
}

// layout takes input over area and returns where a picture of size sz
// shows, zoomed and panned.
func (z *zoomPan) layout(gtx C, area image.Rectangle, sz image.Point) image.Rectangle {
	z.area = area
	if z.zoom <= 0 {
		z.zoom = 1
	}
	func() {
		defer clip.Rect(area).Push(gtx.Ops).Pop()
		if z.zoomed() {
			pointer.CursorGrab.Add(gtx.Ops)
		}
		event.Op(gtx.Ops, z)
	}()
	if sz.X <= 0 || sz.Y <= 0 {
		return area
	}
	s := min(float32(area.Dx())/float32(sz.X), float32(area.Dy())/float32(sz.Y))
	z.fit = f32.Pt(float32(sz.X)*s, float32(sz.Y)*s)
	zoom := z.shownZoom.step(gtx, z.zoom, durPopIn)
	pan := f32.Pt(z.panX.step(gtx, z.pan.X, durPopIn), z.panY.step(gtx, z.pan.Y, durPopIn))
	w, h := z.fit.X*zoom, z.fit.Y*zoom
	c := pointF(area.Min.Add(area.Size().Div(2))).Add(pan)
	return image.Rectangle{
		Min: image.Pt(int(c.X-w/2), int(c.Y-h/2)),
		Max: image.Pt(int(c.X+w/2), int(c.Y+h/2)),
	}
}
