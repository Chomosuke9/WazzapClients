package lottie

import "math"

const (
	capButt   = 1
	capRound  = 2
	capSquare = 3

	joinMiter = 1
	joinRound = 2
	joinBevel = 3
)

// outliner draws closed contours into the raster, through a transform.
type outliner struct {
	ras         *raster
	m           mat
	first, prev vec
	open        bool
}

func (o *outliner) moveTo(p vec) {
	o.close()
	q := o.m.apply(p)
	o.first, o.prev, o.open = q, q, true
}

func (o *outliner) lineTo(p vec) {
	q := o.m.apply(p)
	o.ras.line(o.prev, q)
	o.prev = q
}

func (o *outliner) close() {
	if o.open {
		o.ras.line(o.prev, o.first)
		o.open = false
	}
}

// fillPath draws a path's outline (closed whether or not it is) through m.
func (r *renderer) fillPath(p pathRef, m mat) {
	n := p.segs()
	if n == 0 {
		return
	}
	o := outliner{ras: &r.ras, m: identity}
	s := int(p.start)
	p0 := m.apply(r.pts[s])
	o.moveTo(p0)
	for k := 0; k < n; k++ {
		i := s + 3*k
		c1, c2, p3 := m.apply(r.pts[i+1]), m.apply(r.pts[i+2]), m.apply(r.pts[i+3])
		steps := flatSteps(p0, c1, c2, p3, flatTol)
		for j := 1; j < steps; j++ {
			o.lineTo(cubicAt(p0, c1, c2, p3, float64(j)/float64(steps)))
		}
		o.lineTo(p3)
		p0 = p3
	}
	o.close()
}

// flatTol is how far, in pixels, lines may stray from the curves they draw.
const flatTol = 0.2

// flatten turns a path, through m, into a polyline in r.poly.
func (r *renderer) flatten(p pathRef, m mat, tol float64) []vec {
	r.poly = r.poly[:0]
	s := int(p.start)
	p0 := m.apply(r.pts[s])
	r.poly = append(r.poly, p0)
	for k := 0; k < p.segs(); k++ {
		i := s + 3*k
		c1, c2, p3 := m.apply(r.pts[i+1]), m.apply(r.pts[i+2]), m.apply(r.pts[i+3])
		steps := flatSteps(p0, c1, c2, p3, tol)
		for j := 1; j < steps; j++ {
			r.poly = append(r.poly, cubicAt(p0, c1, c2, p3, float64(j)/float64(steps)))
		}
		r.poly = append(r.poly, p3)
		p0 = p3
	}
	return r.poly
}

// stroker outlines polylines, in the paint's space, and draws the outlines
// into the raster through the paint's transform.
type stroker struct {
	o       outliner
	hw      float64 // half the width
	cap     int
	join    int
	miter   float64
	arcStep float64 // radians per line of a round join or cap
	pts     []vec

	lenIn, lenOut float64 // the lengths of the segments around a join
}

func (s *stroker) setup(hw, scale float64) {
	s.hw = hw
	rdev := hw * scale
	if rdev < 0.5 {
		s.arcStep = math.Pi / 2
		return
	}
	s.arcStep = 2 * math.Acos(math.Max(0, 1-flatTol/rdev))
	s.arcStep = math.Max(math.Min(s.arcStep, math.Pi/4), 0.02)
}

// polyline strokes pts (which it may change).
func (s *stroker) polyline(in []vec, closed bool) {
	// Drop repeated points.
	pts := s.pts[:0]
	for _, p := range in {
		if len(pts) > 0 && near(pts[len(pts)-1], p) {
			continue
		}
		pts = append(pts, p)
	}
	if closed && len(pts) > 1 && near(pts[0], pts[len(pts)-1]) {
		pts = pts[:len(pts)-1]
	}
	s.pts = pts
	switch {
	case len(pts) == 0:
		return
	case len(pts) == 1:
		s.dot(pts[0])
		return
	case len(pts) == 2:
		closed = false
	}
	if closed {
		s.closedSide(pts, false)
		s.closedSide(pts, true)
		return
	}
	n := len(pts)
	o := &s.o
	d0 := pts[1].sub(pts[0]).norm()
	o.moveTo(pts[0].add(d0.rot90().mul(s.hw)))
	for i := 1; i < n-1; i++ {
		s.lenIn, s.lenOut = pts[i].sub(pts[i-1]).len(), pts[i+1].sub(pts[i]).len()
		s.joinAt(pts[i], pts[i].sub(pts[i-1]).norm(), pts[i+1].sub(pts[i]).norm())
	}
	dl := pts[n-1].sub(pts[n-2]).norm()
	o.lineTo(pts[n-1].add(dl.rot90().mul(s.hw)))
	s.capAt(pts[n-1], dl)
	for i := n - 2; i > 0; i-- {
		s.lenIn, s.lenOut = pts[i].sub(pts[i+1]).len(), pts[i-1].sub(pts[i]).len()
		s.joinAt(pts[i], pts[i].sub(pts[i+1]).norm(), pts[i-1].sub(pts[i]).norm())
	}
	o.lineTo(pts[0].add(d0.mul(-1).rot90().mul(s.hw)))
	s.capAt(pts[0], d0.mul(-1))
	o.close()
}

func near(a, b vec) bool {
	return math.Abs(a.x-b.x) < 1e-9 && math.Abs(a.y-b.y) < 1e-9
}

// closedSide outlines one side of a closed polyline, walked forward or
// backward.
func (s *stroker) closedSide(pts []vec, backward bool) {
	n := len(pts)
	at := func(i int) vec {
		i = ((i % n) + n) % n
		if backward {
			return pts[n-1-i]
		}
		return pts[i]
	}
	first := true
	for i := 0; i < n; i++ {
		p := at(i)
		in, out := p.sub(at(i-1)), at(i+1).sub(p)
		s.lenIn, s.lenOut = in.len(), out.len()
		din, dout := in.norm(), out.norm()
		if first {
			s.o.moveTo(p.add(din.rot90().mul(s.hw)))
			first = false
		} else {
			s.o.lineTo(p.add(din.rot90().mul(s.hw)))
		}
		s.joinBody(p, din, dout)
	}
	s.o.close()
}

// joinAt goes from the offset of the segment coming in to that of the one
// going out, at vertex p, on the side left of the direction of travel.
func (s *stroker) joinAt(p, din, dout vec) {
	s.o.lineTo(p.add(din.rot90().mul(s.hw)))
	s.joinBody(p, din, dout)
}

// joinBody draws the join from the incoming offset (already reached) to the
// outgoing one.
func (s *stroker) joinBody(p, din, dout vec) {
	nin, nout := din.rot90(), dout.rot90()
	b := p.add(nout.mul(s.hw))
	turn := din.cross(dout)
	if turn > 0 {
		// The inner side. Where the two offset lines cross within both
		// segments, go back to that crossing: retraced lines cancel out,
		// and nothing overlaps (overlaps darken antialiased edges, which
		// thin strokes are all of). Otherwise through the vertex, which
		// nonzero filling hides.
		cos := nin.dot(nout)
		if 1+cos > 1e-9 {
			back := s.hw * math.Sqrt(math.Max(0, (1-cos)/(1+cos)))
			if back <= s.lenIn && back <= s.lenOut {
				s.o.lineTo(p.add(nin.add(nout).mul(s.hw / (1 + cos))))
				s.o.lineTo(b)
				return
			}
		}
		s.o.lineTo(p)
		s.o.lineTo(b)
		return
	}
	if turn == 0 && din.dot(dout) > 0 {
		s.o.lineTo(b)
		return
	}
	switch s.join {
	case joinMiter:
		cos := nin.dot(nout)
		if 1+cos > 1e-9 {
			ratio := math.Sqrt(2 / (1 + cos))
			if ratio <= s.miter {
				m := p.add(nin.add(nout).mul(s.hw / (1 + cos)))
				s.o.lineTo(m)
			}
		}
		s.o.lineTo(b)
	case joinBevel:
		s.o.lineTo(b)
	default:
		s.arc(p, nin, nout)
		s.o.lineTo(b)
	}
}

// arc draws the arc around p from direction from to direction to, the
// short way.
func (s *stroker) arc(p, from, to vec) {
	a0 := math.Atan2(from.y, from.x)
	sweep := math.Atan2(from.cross(to), from.dot(to))
	steps := int(math.Ceil(math.Abs(sweep) / s.arcStep))
	for i := 1; i < steps; i++ {
		a := a0 + sweep*float64(i)/float64(steps)
		s.o.lineTo(p.add(vec{math.Cos(a), math.Sin(a)}.mul(s.hw)))
	}
}

// capAt ends the line at p, going in direction d, from the left offset
// (reached) to the right one.
func (s *stroker) capAt(p, d vec) {
	n := d.rot90()
	switch s.cap {
	case capSquare:
		e := d.mul(s.hw)
		s.o.lineTo(p.add(n.mul(s.hw)).add(e))
		s.o.lineTo(p.sub(n.mul(s.hw)).add(e))
	case capRound:
		a0 := math.Atan2(n.y, n.x)
		steps := int(math.Ceil(math.Pi / s.arcStep))
		for i := 1; i < steps; i++ {
			a := a0 - math.Pi*float64(i)/float64(steps)
			s.o.lineTo(p.add(vec{math.Cos(a), math.Sin(a)}.mul(s.hw)))
		}
	}
	s.o.lineTo(p.sub(n.mul(s.hw)))
}

// dot draws a path of one point: a round or square cap's shape.
func (s *stroker) dot(p vec) {
	switch s.cap {
	case capRound:
		steps := max(4, int(math.Ceil(2*math.Pi/s.arcStep)))
		s.o.moveTo(p.add(vec{s.hw, 0}))
		for i := 1; i < steps; i++ {
			a := 2 * math.Pi * float64(i) / float64(steps)
			s.o.lineTo(p.add(vec{math.Cos(a), math.Sin(a)}.mul(s.hw)))
		}
		s.o.close()
	case capSquare:
		h := s.hw
		s.o.moveTo(p.add(vec{-h, -h}))
		s.o.lineTo(p.add(vec{h, -h}))
		s.o.lineTo(p.add(vec{h, h}))
		s.o.lineTo(p.add(vec{-h, h}))
		s.o.close()
	}
}

// dasher cuts polylines into dashes.
type dasher struct {
	pattern []float64
	offset  float64
	out     []vec
}

// cut calls fn with each dash of pts (closed adds the closing segment).
func (d *dasher) cut(pts []vec, closed bool, fn func([]vec)) {
	total := 0.0
	for _, l := range d.pattern {
		total += l
	}
	if total <= 0 || len(pts) < 2 {
		fn(pts)
		return
	}
	// Where in the pattern the path starts.
	idx := 0
	pos := math.Mod(d.offset, total)
	if pos < 0 {
		pos += total
	}
	for pos >= d.pattern[idx] {
		pos -= d.pattern[idx]
		idx = (idx + 1) % len(d.pattern)
	}
	left := d.pattern[idx] - pos // of the current dash or gap
	on := idx%2 == 0
	d.out = d.out[:0]
	if on {
		d.out = append(d.out, pts[0])
	}
	n := len(pts)
	segs := n - 1
	if closed {
		segs = n
	}
	for k := 0; k < segs; k++ {
		a, b := pts[k], pts[(k+1)%n]
		seg := b.sub(a).len()
		at := 0.0
		for seg-at > left {
			at += left
			q := lerpv(a, b, at/seg)
			if on {
				d.out = append(d.out, q)
				fn(d.out)
				d.out = d.out[:0]
			} else {
				d.out = append(d.out[:0], q)
			}
			on = !on
			idx = (idx + 1) % len(d.pattern)
			left = d.pattern[idx]
			if left <= 0 && d.pattern[(idx+1)%len(d.pattern)] <= 0 {
				return
			}
		}
		left -= seg - at
		if on {
			d.out = append(d.out, b)
		}
	}
	if on && len(d.out) > 1 {
		fn(d.out)
	}
}
