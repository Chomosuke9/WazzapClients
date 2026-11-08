package lottie

import (
	"image"
	"math"
)

// raster turns lines into coverage, by the signed area each line adds to the
// pixels it crosses (font-rs's accumulation rasterizer). Summing a row's
// deltas from the left gives each pixel's winding-weighted coverage, which
// the fill rule folds into [0, 1].
type raster struct {
	w, h   int
	acc    []float32 // (w+2) per row
	row    []float32
	x0, x1 int // touched columns
	y0, y1 int // touched rows
}

func (r *raster) reset(w, h int) {
	if r.w != w || r.h != h || len(r.acc) != (w+2)*h {
		r.w, r.h = w, h
		r.acc = make([]float32, (w+2)*h)
		r.row = make([]float32, w+2)
	}
	r.x0, r.y0, r.x1, r.y1 = math.MaxInt32, math.MaxInt32, -1, -1
}

// bounds returns the pixels touched since reset.
func (r *raster) bounds() image.Rectangle {
	if r.x1 < r.x0 || r.y1 < r.y0 {
		return image.Rectangle{}
	}
	return image.Rect(r.x0, r.y0, min(r.x1+1, r.w), r.y1+1).Intersect(image.Rect(0, 0, r.w, r.h))
}

// line adds a line, in pixels.
func (r *raster) line(a, b vec) {
	if a.y == b.y || math.IsNaN(a.x+a.y+b.x+b.y) {
		return
	}
	w := float64(r.w)
	// Parts left of the canvas count at its left edge, parts right of it
	// at its right edge: they still wind the pixels in between.
	if (a.x < 0 && b.x > 0) || (a.x > 0 && b.x < 0) {
		y := a.y + (0-a.x)*(b.y-a.y)/(b.x-a.x)
		m := vec{0, y}
		r.line(a, m)
		r.line(m, b)
		return
	}
	if (a.x > w && b.x < w) || (a.x < w && b.x > w) {
		y := a.y + (w-a.x)*(b.y-a.y)/(b.x-a.x)
		m := vec{w, y}
		r.line(a, m)
		r.line(m, b)
		return
	}
	a.x = math.Max(0, math.Min(w, a.x))
	b.x = math.Max(0, math.Min(w, b.x))
	r.edge(a, b)
}

func (r *raster) edge(p0, p1 vec) {
	dir := float32(1)
	if p0.y > p1.y {
		p0, p1 = p1, p0
		dir = -1
	}
	h := float64(r.h)
	if p1.y <= 0 || p0.y >= h {
		return
	}
	dxdy := (p1.x - p0.x) / (p1.y - p0.y)
	x := p0.x
	ystart := p0.y
	if ystart < 0 {
		x -= ystart * dxdy
		ystart = 0
	}
	yend := math.Min(p1.y, h)
	stride := r.w + 2
	maxX := float64(r.w)
	yi0 := int(ystart)
	yi1 := int(math.Ceil(yend)) - 1
	if yi0 < r.y0 {
		r.y0 = yi0
	}
	if yi1 > r.y1 {
		r.y1 = yi1
	}
	for y := yi0; float64(y) < yend; y++ {
		top := math.Max(float64(y), ystart)
		bot := math.Min(float64(y+1), yend)
		dy := bot - top
		xnext := x + dxdy*dy
		xnext = math.Max(0, math.Min(maxX, xnext))
		d := float32(dy) * dir
		x0, x1 := x, xnext
		if x0 > x1 {
			x0, x1 = x1, x0
		}
		x0f := math.Floor(x0)
		x0i := int(x0f)
		x1c := math.Ceil(x1)
		x1i := int(x1c)
		a := r.acc[y*stride:]
		if x0i < r.x0 {
			r.x0 = x0i
		}
		if x1i > r.x1 {
			r.x1 = x1i
		}
		if x1i <= x0i+1 {
			xmf := float32(0.5*(x+xnext) - x0f)
			a[x0i] += d - d*xmf
			a[x0i+1] += d * xmf
		} else {
			s := float32(1 / (x1 - x0))
			x0r := float32(x0 - x0f)
			a0 := 0.5 * s * (1 - x0r) * (1 - x0r)
			x1r := float32(x1 - x1c + 1)
			am := 0.5 * s * x1r * x1r
			a[x0i] += d * a0
			if x1i == x0i+2 {
				a[x0i+1] += d * (1 - a0 - am)
			} else {
				a1 := s * (1.5 - x0r)
				a[x0i+1] += d * (a1 - a0)
				for xi := x0i + 2; xi < x1i-1; xi++ {
					a[xi] += d * s
				}
				a2 := a1 + float32(x1i-x0i-3)*s
				a[x1i-1] += d * (1 - a2 - am)
			}
			a[x1i] += d * am
		}
		x = xnext
	}
}

// scan hands each touched row's coverage, folded by the fill rule, to fn,
// and clears the accumulation.
func (r *raster) scan(evenOdd bool, fn func(y, x0 int, cov []float32)) {
	if r.x1 < r.x0 || r.y1 < r.y0 {
		return
	}
	stride := r.w + 2
	x0 := max(r.x0, 0)
	x1 := min(r.x1+1, stride-1) // last delta index, inclusive
	px1 := min(x1, r.w-1)       // last pixel
	y0, y1 := max(r.y0, 0), min(r.y1, r.h-1)
	for y := y0; y <= y1; y++ {
		a := r.acc[y*stride:]
		row := r.row[:px1-x0+1]
		var sum float32
		hit := false
		for x := x0; x <= x1; x++ {
			sum += a[x]
			a[x] = 0
			if x > px1 {
				continue
			}
			c := sum
			if c < 0 {
				c = -c
			}
			if evenOdd {
				c = float32(math.Mod(float64(c), 2))
				if c > 1 {
					c = 2 - c
				}
			} else if c > 1 {
				c = 1
			}
			if c < 1.0/512 {
				c = 0
			} else {
				hit = true
			}
			row[x-x0] = c
		}
		if hit {
			fn(y, x0, row)
		}
	}
	r.x0, r.y0, r.x1, r.y1 = math.MaxInt32, math.MaxInt32, -1, -1
}

// layerBuf is a premultiplied RGBA picture the size of the frame, with
// the part drawn on.
type layerBuf struct {
	pix   []uint8
	w, h  int
	dirty image.Rectangle
}

func (b *layerBuf) touch(r image.Rectangle) {
	if r.Empty() {
		return
	}
	if b.dirty.Empty() {
		b.dirty = r
	} else {
		b.dirty = b.dirty.Union(r)
	}
}

// clear clears the part drawn on.
func (b *layerBuf) clear() {
	d := b.dirty
	for y := d.Min.Y; y < d.Max.Y; y++ {
		row := b.pix[(y*b.w+d.Min.X)*4 : (y*b.w+d.Max.X)*4]
		clear(row)
	}
	b.dirty = image.Rectangle{}
}

// over draws src over b at opacity op.
func (b *layerBuf) over(src *layerBuf, op float32) {
	d := src.dirty
	if d.Empty() || op <= 0 {
		return
	}
	for y := d.Min.Y; y < d.Max.Y; y++ {
		i := (y*b.w + d.Min.X) * 4
		s := src.pix[i : i+d.Dx()*4]
		t := b.pix[i : i+d.Dx()*4]
		for j := 0; j < len(s); j += 4 {
			sa := s[j+3]
			if sa == 0 {
				continue
			}
			if sa == 255 && op >= 1 {
				t[j], t[j+1], t[j+2], t[j+3] = s[j], s[j+1], s[j+2], 255
				continue
			}
			inv := 1 - float32(sa)*op/255
			t[j] = u8(float32(s[j])*op + float32(t[j])*inv)
			t[j+1] = u8(float32(s[j+1])*op + float32(t[j+1])*inv)
			t[j+2] = u8(float32(s[j+2])*op + float32(t[j+2])*inv)
			t[j+3] = u8(float32(sa)*op + float32(t[j+3])*inv)
		}
	}
	b.touch(d)
}

func u8(v float32) uint8 {
	if v >= 254.5 {
		return 255
	}
	if v <= 0 {
		return 0
	}
	return uint8(v + 0.5)
}

const (
	shadeSolid = iota
	shadeLinear
	shadeRadial
)

// shader colors a fill: a color, or a gradient through a lookup table.
type shader struct {
	kind int
	// premultiplied color, alpha included, 0..1
	r, g, b, a float32
	lut        *[256][4]float32
	// linear: t = ax*x + ay*y + a0 at a pixel's center
	ax, ay, a0 float64
	// radial: device to gradient space, the focal point, f - center, radius
	inv  mat
	foc  vec
	d    vec
	rad  float64
	quad float64 // d·d - rad²
}

func (s *shader) at(x, y int) (r, g, b, a float32) {
	var t float64
	px, py := float64(x)+0.5, float64(y)+0.5
	switch s.kind {
	case shadeLinear:
		t = s.ax*px + s.ay*py + s.a0
	case shadeRadial:
		q := s.inv.apply(vec{px, py}).sub(s.foc)
		qd := q.dot(s.d)
		if s.quad == 0 {
			if qd != 0 {
				t = q.dot(q) / (2 * qd)
			}
		} else {
			disc := qd*qd - s.quad*q.dot(q)
			if disc < 0 {
				disc = 0
			}
			t = (qd - math.Sqrt(disc)) / s.quad
		}
	}
	i := int(t*255 + 0.5)
	if i < 0 || t != t {
		i = 0
	} else if i > 255 {
		i = 255
	}
	c := &s.lut[i]
	return c[0], c[1], c[2], c[3]
}

// span blends a row of coverage onto b.
func (b *layerBuf) span(y, x0 int, cov []float32, s *shader) {
	p := b.pix[(y*b.w+x0)*4:]
	solid := s.kind == shadeSolid
	cr, cg, cb, ca := s.r, s.g, s.b, s.a
	for i, c := range cov {
		if c == 0 {
			continue
		}
		if !solid {
			cr, cg, cb, ca = s.at(x0+i, y)
		}
		a := ca * c
		if a <= 0 {
			continue
		}
		j := i * 4
		inv := 1 - a
		p[j] = u8(cr*c*255 + float32(p[j])*inv)
		p[j+1] = u8(cg*c*255 + float32(p[j+1])*inv)
		p[j+2] = u8(cb*c*255 + float32(p[j+2])*inv)
		p[j+3] = u8(a*255 + float32(p[j+3])*inv)
	}
}
