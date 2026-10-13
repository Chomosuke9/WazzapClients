package icon

import (
	"math"

	"gioui.org/f32"
)

// StickerSmiley is the composer's button for the expressions panel after
// the sticker tab was used there: WhatsApp's smiley with its bottom right
// corner peeled off, like a sticker. Material Symbols has no such glyph, so
// it's drawn from strokes in the unit square.
var StickerSmiley = stickerSmiley()

func stickerSmiley() *Icon {
	// In the box the face fills, (0,0) to (1,1).
	const (
		lo, size = 0.1, 0.8
		r        = 0.27  // the corners' radius
		w        = 0.088 // stroke width, about Material Symbols' weight 500
	)
	pt := func(u, v float64) f32.Point { return f32.Pt(float32(lo+size*u), float32(lo+size*v)) }
	arc := func(pts []f32.Point, cu, cv, rad, from, to float64) []f32.Point {
		for i := 0; i <= 8; i++ {
			a := (from + (to-from)*float64(i)/8) * math.Pi / 180
			pts = append(pts, pt(cu+rad*math.Cos(a), cv+rad*math.Sin(a)))
		}
		return pts
	}
	// A rounded square whose bottom right corner is cut off from A to B
	// (peeled), and the peeled corner folded back over the face (the
	// triangle A, B, fold).
	a, b, fold := pt(1, 0.52), pt(0.5, 1), pt(0.67, 0.69)
	var face []f32.Point
	face = append(face, b)
	face = arc(face, r, 1-r, r, 90, 180)
	face = arc(face, r, r, r, 180, 270)
	face = arc(face, 1-r, r, r, 270, 360)
	face = append(face, a)

	ic := &Icon{}
	stroke(ic, face, w*size)
	stroke(ic, []f32.Point{a, fold, b, a}, w*size)
	polygon(ic, []f32.Point{a, b, fold})
	// An open smile: the lower half of an ellipse.
	var mouth []f32.Point
	for d := 0.0; d <= 180.01; d += 10 {
		x := d * math.Pi / 180
		mouth = append(mouth, pt(0.44+0.19*math.Cos(x), 0.56+0.17*math.Sin(x)))
	}
	polygon(ic, mouth)
	disc(ic, pt(0.31, 0.33), 0.065*size)
	disc(ic, pt(0.62, 0.33), 0.065*size)
	return ic
}

// polygon adds pts as a closed outline, always turned the same way: the
// rasterizer adds up the windings, so outlines that overlap merge.
func polygon(ic *Icon, pts []f32.Point) {
	var area float32
	for i, p := range pts {
		q := pts[(i+1)%len(pts)]
		area += p.X*q.Y - q.X*p.Y
	}
	if area < 0 {
		rev := make([]f32.Point, len(pts))
		for i, p := range pts {
			rev[len(pts)-1-i] = p
		}
		pts = rev
	}
	ic.segs = append(ic.segs, seg{op: 'M', a: pts[0]})
	for _, p := range pts[1:] {
		ic.segs = append(ic.segs, seg{op: 'L', a: p})
	}
	ic.segs = append(ic.segs, seg{op: 'Z'})
}

// stroke adds the stroke along the line pts: a bar per segment and a
// disc on every point, which rounds the joins and the ends.
func stroke(ic *Icon, pts []f32.Point, w float32) {
	for i, p := range pts {
		disc(ic, p, w/2)
		if i == 0 {
			continue
		}
		q := pts[i-1]
		v := p.Sub(q)
		l := float32(math.Hypot(float64(v.X), float64(v.Y)))
		n := f32.Pt(-v.Y/l*w/2, v.X/l*w/2)
		polygon(ic, []f32.Point{q.Add(n), p.Add(n), p.Sub(n), q.Sub(n)})
	}
}

// disc adds a filled circle.
func disc(ic *Icon, c f32.Point, r float32) {
	var pts []f32.Point
	for d := 0; d < 360; d += 15 {
		a := float64(d) * math.Pi / 180
		pts = append(pts, f32.Pt(c.X+r*float32(math.Cos(a)), c.Y+r*float32(math.Sin(a))))
	}
	polygon(ic, pts)
}
