package lottie

import "math"

type vec struct{ x, y float64 }

func (a vec) add(b vec) vec         { return vec{a.x + b.x, a.y + b.y} }
func (a vec) sub(b vec) vec         { return vec{a.x - b.x, a.y - b.y} }
func (a vec) mul(s float64) vec     { return vec{a.x * s, a.y * s} }
func (a vec) dot(b vec) float64     { return a.x*b.x + a.y*b.y }
func (a vec) cross(b vec) float64   { return a.x*b.y - a.y*b.x }
func (a vec) len() float64          { return math.Hypot(a.x, a.y) }
func (a vec) rot90() vec            { return vec{-a.y, a.x} }
func lerpv(a, b vec, t float64) vec { return vec{a.x + (b.x-a.x)*t, a.y + (b.y-a.y)*t} }

func (a vec) norm() vec {
	l := a.len()
	if l == 0 {
		return vec{}
	}
	return vec{a.x / l, a.y / l}
}

// mat is an affine transform: x' = a*x + c*y + e, y' = b*x + d*y + f.
type mat struct{ a, b, c, d, e, f float64 }

var identity = mat{a: 1, d: 1}

func (m mat) apply(p vec) vec {
	return vec{m.a*p.x + m.c*p.y + m.e, m.b*p.x + m.d*p.y + m.f}
}

// then returns the transform that applies m, then n.
func (m mat) then(n mat) mat {
	return mat{
		a: n.a*m.a + n.c*m.b,
		b: n.b*m.a + n.d*m.b,
		c: n.a*m.c + n.c*m.d,
		d: n.b*m.c + n.d*m.d,
		e: n.a*m.e + n.c*m.f + n.e,
		f: n.b*m.e + n.d*m.f + n.f,
	}
}

func (m mat) invert() (mat, bool) {
	det := m.a*m.d - m.b*m.c
	if det == 0 || math.IsNaN(det) || math.IsInf(det, 0) {
		return mat{}, false
	}
	return mat{
		a: m.d / det,
		b: -m.b / det,
		c: -m.c / det,
		d: m.a / det,
		e: (m.c*m.f - m.d*m.e) / det,
		f: (m.b*m.e - m.a*m.f) / det,
	}, true
}

// scale returns the most the transform stretches a length.
func (m mat) scale() float64 {
	s := m.a*m.a + m.b*m.b + m.c*m.c + m.d*m.d
	det := m.a*m.d - m.b*m.c
	return math.Sqrt((s + math.Sqrt(math.Max(0, s*s-4*det*det))) / 2)
}

func translate(x, y float64) mat { return mat{a: 1, d: 1, e: x, f: y} }
func scaling(x, y float64) mat   { return mat{a: x, d: y} }
func shearX(k float64) mat       { return mat{a: 1, c: k, d: 1} }

// rotation turns by rad, clockwise on screen (y points down).
func rotation(rad float64) mat {
	s, c := math.Sincos(rad)
	return mat{a: c, b: s, c: -s, d: c}
}

func rad(deg float64) float64 { return deg * math.Pi / 180 }

func cubicAt(p0, c1, c2, p3 vec, t float64) vec {
	u := 1 - t
	a, b, c, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
	return vec{a*p0.x + b*c1.x + c*c2.x + d*p3.x, a*p0.y + b*c1.y + c*c2.y + d*p3.y}
}

// subCubic returns the part of a cubic from t0 to t1.
func subCubic(p0, c1, c2, p3 vec, t0, t1 float64) (vec, vec, vec, vec) {
	if t1 < 1 {
		p0, c1, c2, p3 = splitLeft(p0, c1, c2, p3, t1)
		if t1 > 0 {
			t0 /= t1
		}
	}
	if t0 > 0 {
		p0, c1, c2, p3 = splitRight(p0, c1, c2, p3, t0)
	}
	return p0, c1, c2, p3
}

func splitLeft(p0, c1, c2, p3 vec, t float64) (vec, vec, vec, vec) {
	a := lerpv(p0, c1, t)
	b := lerpv(c1, c2, t)
	c := lerpv(c2, p3, t)
	d := lerpv(a, b, t)
	e := lerpv(b, c, t)
	return p0, a, d, lerpv(d, e, t)
}

func splitRight(p0, c1, c2, p3 vec, t float64) (vec, vec, vec, vec) {
	a := lerpv(p0, c1, t)
	b := lerpv(c1, c2, t)
	c := lerpv(c2, p3, t)
	d := lerpv(a, b, t)
	e := lerpv(b, c, t)
	return lerpv(d, e, t), e, c, p3
}

// flatSteps returns how many lines a cubic is drawn with so that none
// strays more than tol from it (Wang's formula).
func flatSteps(p0, c1, c2, p3 vec, tol float64) int {
	if c1 == p0 && c2 == p3 {
		return 1
	}
	dd := math.Max(p0.sub(c1.mul(2)).add(c2).len(), c1.sub(c2.mul(2)).add(p3).len())
	n := math.Ceil(math.Sqrt(0.75 * dd / tol))
	switch {
	case !(n >= 1):
		return 1
	case n > 512:
		return 512
	}
	return int(n)
}

// cubicLen measures a cubic in lenSteps lines, filling ts with the length
// at each step when it isn't nil.
const lenSteps = 16

func cubicLen(p0, c1, c2, p3 vec, lens *[lenSteps + 1]float64) float64 {
	if c1 == p0 && c2 == p3 {
		l := p3.sub(p0).len()
		if lens != nil {
			for i := range lens {
				lens[i] = l * float64(i) / lenSteps
			}
		}
		return l
	}
	total := 0.0
	prev := p0
	if lens != nil {
		lens[0] = 0
	}
	for i := 1; i <= lenSteps; i++ {
		q := cubicAt(p0, c1, c2, p3, float64(i)/lenSteps)
		total += q.sub(prev).len()
		prev = q
		if lens != nil {
			lens[i] = total
		}
	}
	return total
}

// lenToT returns the parameter at length l along a cubic measured by
// cubicLen.
func lenToT(lens *[lenSteps + 1]float64, l float64) float64 {
	if l <= 0 {
		return 0
	}
	if l >= lens[lenSteps] {
		return 1
	}
	for i := 1; i <= lenSteps; i++ {
		if lens[i] >= l {
			seg := lens[i] - lens[i-1]
			f := 0.0
			if seg > 0 {
				f = (l - lens[i-1]) / seg
			}
			return (float64(i-1) + f) / lenSteps
		}
	}
	return 1
}

// pathRef is a path in the renderer's arena (renderer.pts): a start point,
// then three points (two controls and the end) per cubic segment.
type pathRef struct {
	start, end int32
	closed     bool
}

func (p pathRef) segs() int { return int(p.end-p.start-1) / 3 }

// path building, into the renderer's arena.

func (r *renderer) begin(p vec) {
	r.cur = int32(len(r.pts))
	r.pts = append(r.pts, p)
}

func (r *renderer) cubicTo(c1, c2, p vec) {
	r.pts = append(r.pts, c1, c2, p)
}

func (r *renderer) lineTo(p vec) {
	last := r.pts[len(r.pts)-1]
	r.pts = append(r.pts, last, p, p)
}

func (r *renderer) end(closed bool) pathRef {
	return pathRef{start: r.cur, end: int32(len(r.pts)), closed: closed}
}

// addBez adds a Lottie path (tangents relative to their vertices).
func (r *renderer) addBez(z *bez) (pathRef, bool) {
	n := min(len(z.v), len(z.i), len(z.o))
	if n == 0 {
		return pathRef{}, false
	}
	r.begin(z.v[0])
	for k := 0; k+1 < n; k++ {
		r.cubicTo(z.v[k].add(z.o[k]), z.v[k+1].add(z.i[k+1]), z.v[k+1])
	}
	if z.closed {
		r.cubicTo(z.v[n-1].add(z.o[n-1]), z.v[0].add(z.i[0]), z.v[0])
	}
	return r.end(z.closed), true
}

// addTriples adds a closed path from vertices with absolute out (o) and
// in (i) control points, as lottie-web builds rectangles and stars.
func (r *renderer) addTriples(v, o, in []vec) pathRef {
	n := len(v)
	r.begin(v[0])
	for k := 0; k < n; k++ {
		j := (k + 1) % n
		r.cubicTo(o[k], in[j], v[j])
	}
	return r.end(true)
}

// kappa is how far a quarter circle's control points sit, of its radius.
const kappa = 0.5519

func (r *renderer) addEllipse(c, size vec, ccw bool) pathRef {
	sx, sy := size.x/2, size.y/2
	k := 1.0
	if ccw {
		k = -1
	}
	px, py := c.x, c.y
	v0 := vec{px, py - sy}
	v1 := vec{px + k*sx, py}
	v2 := vec{px, py + sy}
	v3 := vec{px - k*sx, py}
	r.begin(v0)
	r.cubicTo(vec{px + k*sx*kappa, py - sy}, vec{px + k*sx, py - sy*kappa}, v1)
	r.cubicTo(vec{px + k*sx, py + sy*kappa}, vec{px + k*sx*kappa, py + sy}, v2)
	r.cubicTo(vec{px - k*sx*kappa, py + sy}, vec{px - k*sx, py + sy*kappa}, v3)
	r.cubicTo(vec{px - k*sx, py - sy*kappa}, vec{px - k*sx*kappa, py - sy}, v0)
	return r.end(true)
}

func (r *renderer) addRect(c, size vec, round float64, ccw bool) pathRef {
	w, h := size.x/2, size.y/2
	R, L, T, B := c.x+w, c.x-w, c.y-h, c.y+h
	round = math.Max(0, math.Min(round, math.Min(math.Abs(w), math.Abs(h))))
	if round == 0 {
		r.begin(vec{R, T})
		if ccw {
			r.lineTo(vec{L, T})
			r.lineTo(vec{L, B})
			r.lineTo(vec{R, B})
		} else {
			r.lineTo(vec{R, B})
			r.lineTo(vec{L, B})
			r.lineTo(vec{L, T})
		}
		r.lineTo(vec{R, T})
		return r.end(true)
	}
	q := round * (1 - kappa)
	if !ccw {
		r.begin(vec{R, T + round})
		r.lineTo(vec{R, B - round})
		r.cubicTo(vec{R, B - q}, vec{R - q, B}, vec{R - round, B})
		r.lineTo(vec{L + round, B})
		r.cubicTo(vec{L + q, B}, vec{L, B - q}, vec{L, B - round})
		r.lineTo(vec{L, T + round})
		r.cubicTo(vec{L, T + q}, vec{L + q, T}, vec{L + round, T})
		r.lineTo(vec{R - round, T})
		r.cubicTo(vec{R - q, T}, vec{R, T + q}, vec{R, T + round})
		return r.end(true)
	}
	r.begin(vec{R, T + round})
	r.cubicTo(vec{R, T + q}, vec{R - q, T}, vec{R - round, T})
	r.lineTo(vec{L + round, T})
	r.cubicTo(vec{L + q, T}, vec{L, T + q}, vec{L, T + round})
	r.lineTo(vec{L, B - round})
	r.cubicTo(vec{L, B - q}, vec{L + q, B}, vec{L + round, B})
	r.lineTo(vec{R - round, B})
	r.cubicTo(vec{R - q, B}, vec{R, B - q}, vec{R, B - round})
	r.lineTo(vec{R, T + round})
	return r.end(true)
}

// addStar adds a star (polygon false) or a polygon, as lottie-web does.
func (r *renderer) addStar(c vec, points float64, outer, inner, outerRound, innerRound, rotDeg float64, polygon, ccw bool) (pathRef, bool) {
	n := int(math.Floor(points))
	if !polygon {
		n *= 2
	}
	if n < 2 || n > 4096 {
		return pathRef{}, false
	}
	dir := 1.0
	if ccw {
		dir = -1
	}
	angle := 2 * math.Pi / float64(n)
	cur := -math.Pi/2 + rad(rotDeg)
	r.tv, r.to, r.ti = r.tv[:0], r.to[:0], r.ti[:0]
	long := true
	for i := 0; i < n; i++ {
		rd, rnd := outer, outerRound
		perim := 2 * math.Pi * outer / float64(n*4)
		if !polygon {
			perim = 2 * math.Pi * outer / float64(n*2)
			if !long {
				rd, rnd = inner, innerRound
				perim = 2 * math.Pi * inner / float64(n*2)
			}
		}
		x, y := rd*math.Cos(cur), rd*math.Sin(cur)
		var ox, oy float64
		if l := math.Hypot(x, y); l > 0 {
			ox, oy = y/l, -x/l
		}
		x += c.x
		y += c.y
		t := perim * rnd * dir
		r.tv = append(r.tv, vec{x, y})
		r.to = append(r.to, vec{x - ox*t, y - oy*t})
		r.ti = append(r.ti, vec{x + ox*t, y + oy*t})
		long = !long
		cur += angle * dir
	}
	return r.addTriples(r.tv, r.to, r.ti), true
}

// pathLen measures a path, filling r.segLens with its segments' tables.
func (r *renderer) pathLen(p pathRef) float64 {
	n := p.segs()
	if cap(r.segLens) < n {
		r.segLens = make([][lenSteps + 1]float64, n)
	}
	r.segLens = r.segLens[:n]
	total := 0.0
	for k := 0; k < n; k++ {
		i := int(p.start) + 3*k
		total += cubicLen(r.pts[i], r.pts[i+1], r.pts[i+2], r.pts[i+3], &r.segLens[k])
	}
	return total
}

// subPath adds the part of p from length a to b (measured by pathLen) to
// the path being built, starting it when start is set.
func (r *renderer) subPath(p pathRef, a, b float64, start bool) {
	acc := 0.0
	for k := 0; k < p.segs(); k++ {
		l := r.segLens[k][lenSteps]
		s0, s1 := acc, acc+l
		acc = s1
		if s1 < a || s0 > b {
			continue
		}
		i := int(p.start) + 3*k
		t0, t1 := 0.0, 1.0
		if a > s0 {
			t0 = lenToT(&r.segLens[k], a-s0)
		}
		if b < s1 {
			t1 = lenToT(&r.segLens[k], b-s0)
		}
		q0, q1, q2, q3 := subCubic(r.pts[i], r.pts[i+1], r.pts[i+2], r.pts[i+3], t0, t1)
		if start {
			r.begin(q0)
			start = false
		}
		r.cubicTo(q1, q2, q3)
	}
	if start {
		// Nothing of it: a path of one point.
		q := r.pts[p.start]
		r.begin(q)
	}
}

// roundCorners rounds a path's sharp corners, as lottie-web does.
func (r *renderer) roundCorners(p pathRef, radius float64) pathRef {
	n := p.segs()
	if n == 0 {
		return p
	}
	// Vertices with absolute in and out controls.
	r.tv, r.to, r.ti = r.tv[:0], r.to[:0], r.ti[:0]
	s := int(p.start)
	for k := 0; k <= n; k++ {
		v := r.pts[s+3*k]
		in, out := v, v
		if k > 0 {
			in = r.pts[s+3*k-1]
		}
		if k < n {
			out = r.pts[s+3*k+1]
		}
		r.tv = append(r.tv, v)
		r.ti = append(r.ti, in)
		r.to = append(r.to, out)
	}
	m := len(r.tv)
	if p.closed && m > 1 && r.tv[m-1] == r.tv[0] {
		r.ti[0] = r.ti[m-1]
		r.tv, r.ti, r.to = r.tv[:m-1], r.ti[:m-1], r.to[:m-1]
		m--
	}
	r.rv = r.rv[:0]
	for k := 0; k < m; k++ {
		v := r.tv[k]
		sharp := r.ti[k] == v && r.to[k] == v
		endpoint := !p.closed && (k == 0 || k == m-1)
		if !sharp || endpoint || m < 3 {
			r.rv = append(r.rv, roundVtx{r.tv[k], r.ti[k], r.to[k]})
			continue
		}
		prev := r.tv[(k+m-1)%m]
		next := r.tv[(k+1)%m]
		a := toward(v, prev, radius)
		b := toward(v, next, radius)
		r.rv = append(r.rv,
			roundVtx{a, a, lerpv(a, v, kappa)},
			roundVtx{b, lerpv(b, v, kappa), b})
	}
	rv := r.rv
	r.begin(rv[0].v)
	for k := 0; k+1 < len(rv); k++ {
		r.cubicTo(rv[k].out, rv[k+1].in, rv[k+1].v)
	}
	if p.closed {
		l := len(rv) - 1
		r.cubicTo(rv[l].out, rv[0].in, rv[0].v)
	}
	return r.end(p.closed)
}

type roundVtx struct{ v, in, out vec }

// toward returns the point at most radius (and half the way) from v to w.
func toward(v, w vec, radius float64) vec {
	d := w.sub(v).len()
	if d == 0 {
		return v
	}
	return lerpv(v, w, math.Min(d/2, radius)/d)
}
