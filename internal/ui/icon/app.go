package icon

import (
	"image"
	"image/color"
	"math"

	"golang.org/x/image/vector"
)

// App draws the app's icon px pixels square: a white chat bubble with a
// lightning bolt in it, on a green rounded square.
func App(px int) *image.RGBA { return appIcon(px, true) }

// Tray draws the tray's icon: App's white bubble and bolt alone, on
// nothing, grown to fill the square.
func Tray(px int) *image.RGBA { return appIcon(px, false) }

func appIcon(px int, withTile bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, px, px))
	z := vector.NewRasterizer(px, px)
	// The drawing is laid out in a square of side s at ox, oy.
	s, ox, oy := float32(px), float32(0), float32(0)

	// The tile, green fading a little darker downwards.
	var hole image.Image // the tile, if any
	if withTile {
		tile := vgradient(px, color.NRGBA{R: 0x2c, G: 0xc9, B: 0x6e, A: 0xff},
			color.NRGBA{R: 0x1d, G: 0xaa, B: 0x61, A: 0xff})
		roundRect(z, 0, 0, s, s, 0.225*s)
		z.Draw(img, img.Bounds(), tile, image.Point{})
		hole = tile
	} else {
		// The bubble spans 0.675 of the tile, centered at 0.5, 0.4925.
		s = float32(px) * 0.98 / 0.675
		ox, oy = float32(px)/2-0.5*s, float32(px)/2-0.4925*s
	}

	// The bubble: a ring with a tail at its bottom left. The inner circle
	// winds the other way, which cuts the hole.
	white := image.NewUniform(color.White)
	cx, cy := ox+0.5*s, oy+0.49*s
	ro := 0.335 * s
	w := max(0.05*s, 1.3) // the ring's width, at least 1.3 px when tiny
	ri := ro - w
	z.Reset(px, px)
	circle(z, cx, cy, ro, false)
	circle(z, cx, cy, ri, true)
	mid := (ro + ri) / 2
	at := func(deg float64, r float32) (float32, float32) {
		a := deg * math.Pi / 180
		return cx + r*float32(math.Cos(a)), cy + r*float32(math.Sin(a))
	}
	ax, ay := at(112, mid)
	bx, by := at(158, mid)
	tx, ty := ox+0.17*s, oy+0.83*s
	z.MoveTo(ax, ay)
	z.LineTo(tx, ty)
	z.LineTo(bx, by)
	z.ClosePath()
	z.Draw(img, img.Bounds(), white, image.Point{})

	// The tail is hollow like the ring: a triangle of the tile (or of
	// nothing), its sides w inside the white one's, opens it into the hole.
	if g, ok := tailHole(cx, cy, ri, w, [2]float32{ax, ay}, [2]float32{bx, by}, [2]float32{tx, ty}); ok {
		z.Reset(px, px)
		z.MoveTo(g[0][0], g[0][1])
		z.LineTo(g[1][0], g[1][1])
		z.LineTo(g[2][0], g[2][1])
		z.ClosePath()
		if hole != nil {
			z.Draw(img, img.Bounds(), hole, image.Point{})
		} else {
			// Erase: the rasterizer's Src would clear the whole image.
			m := image.NewAlpha(img.Bounds())
			z.Draw(m, m.Bounds(), image.Opaque, image.Point{})
			for i, a := range m.Pix {
				for c := range 4 {
					p := &img.Pix[4*i+c]
					*p = uint8(uint32(*p) * uint32(255-a) / 255)
				}
			}
		}
	}

	// The bolt.
	bolt := [][2]float32{
		{0.045, -0.175}, {-0.105, 0.025}, {-0.005, 0.025},
		{-0.045, 0.175}, {0.105, -0.030}, {0.005, -0.030},
	}
	b := 1.1 * s
	if px <= 24 {
		b = 1.25 * s // tray sizes: more bolt, or it's a smudge
	}
	z.Reset(px, px)
	for i, p := range bolt {
		x, y := cx+p[0]*b, cy+p[1]*b
		if i == 0 {
			z.MoveTo(x, y)
		} else {
			z.LineTo(x, y)
		}
	}
	z.ClosePath()
	z.Draw(img, img.Bounds(), white, image.Point{})
	return img
}

// circle adds a circle of cubics, clockwise on screen or, with ccw,
// counterclockwise.
func circle(z *vector.Rasterizer, cx, cy, r float32, ccw bool) {
	k := 0.5523 * r
	d := r // y of the second quarter's end: down (clockwise) or up
	if ccw {
		d = -r
	}
	kd := 0.5523 * d
	z.MoveTo(cx+r, cy)
	z.CubeTo(cx+r, cy+kd, cx+k, cy+d, cx, cy+d)
	z.CubeTo(cx-k, cy+d, cx-r, cy+kd, cx-r, cy)
	z.CubeTo(cx-r, cy-kd, cx-k, cy-d, cx, cy-d)
	z.CubeTo(cx+k, cy-d, cx+r, cy-kd, cx+r, cy)
	z.ClosePath()
}

// tailHole returns the triangle that hollows out the tail a, t, b (t its
// tip): its sides run w inside the tail's two sides, from their meeting
// point to just inside the ring's hole (radius ri around cx, cy).
func tailHole(cx, cy, ri, w float32, a, b, t [2]float32) ([3][2]float32, bool) {
	type v = [2]float32
	sub := func(p, q v) v { return v{p[0] - q[0], p[1] - q[1]} }
	add := func(p, q v, k float32) v { return v{p[0] + k*q[0], p[1] + k*q[1]} }
	dot := func(p, q v) float32 { return p[0]*q[0] + p[1]*q[1] }
	cross := func(p, q v) float32 { return p[0]*q[1] - p[1]*q[0] }
	unit := func(p v) v {
		l := float32(math.Hypot(float64(p[0]), float64(p[1])))
		return v{p[0] / l, p[1] / l}
	}
	// A side from the tip towards p, moved w towards the tail's inside
	// (towards q).
	inset := func(p, q v) (v, v) {
		d := unit(sub(p, t))
		n := v{-d[1], d[0]}
		if dot(n, sub(q, t)) < 0 {
			n = v{d[1], -d[0]}
		}
		return add(t, n, w), d
	}
	pa, da := inset(a, b)
	pb, db := inset(b, a)
	den := cross(da, db)
	if den == 0 {
		return [3][2]float32{}, false
	}
	g := add(pa, da, cross(sub(pb, pa), db)/den) // where the two sides meet
	// Follow a side from g until it's a little inside the hole.
	c, r := v{cx, cy}, ri-0.3*w
	reach := func(d v) (v, bool) {
		o := sub(g, c)
		bq, cq := dot(o, d), dot(o, o)-r*r
		disc := bq*bq - cq
		if disc < 0 {
			return v{}, false
		}
		k := -bq - float32(math.Sqrt(float64(disc)))
		if k <= 0 {
			return v{}, false
		}
		return add(g, d, k), true
	}
	ea, ok1 := reach(da)
	eb, ok2 := reach(db)
	return [3][2]float32{ea, g, eb}, ok1 && ok2
}

// roundRect adds a rectangle with corners of radius r.
func roundRect(z *vector.Rasterizer, x0, y0, x1, y1, r float32) {
	k := 0.4477 * r // 1 - 0.5523: the control points' distance from the corner
	z.MoveTo(x0+r, y0)
	z.LineTo(x1-r, y0)
	z.CubeTo(x1-k, y0, x1, y0+k, x1, y0+r)
	z.LineTo(x1, y1-r)
	z.CubeTo(x1, y1-k, x1-k, y1, x1-r, y1)
	z.LineTo(x0+r, y1)
	z.CubeTo(x0+k, y1, x0, y1-k, x0, y1-r)
	z.LineTo(x0, y0+r)
	z.CubeTo(x0, y0+k, x0+k, y0, x0+r, y0)
	z.ClosePath()
}

// vgradient is a px square image fading from top to bottom.
func vgradient(px int, top, bottom color.NRGBA) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 1, px))
	mix := func(a, b uint8, t float32) uint8 { return uint8(float32(a) + (float32(b)-float32(a))*t + 0.5) }
	for y := range px {
		t := float32(y) / float32(max(px-1, 1))
		img.SetNRGBA(0, y, color.NRGBA{R: mix(top.R, bottom.R, t), G: mix(top.G, bottom.G, t),
			B: mix(top.B, bottom.B, t), A: 0xff})
	}
	return &stretchX{img}
}

// stretchX repeats a one pixel wide image sideways.
type stretchX struct{ *image.NRGBA }

func (s *stretchX) Bounds() image.Rectangle {
	return image.Rect(0, 0, math.MaxInt32, s.NRGBA.Bounds().Dy())
}

func (s *stretchX) At(x, y int) color.Color { return s.NRGBA.At(0, y) }
