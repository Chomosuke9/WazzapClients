package lottie

import "math"

// prop is an animatable property: a number or a vector, static or
// keyframed.
type prop struct {
	static []float64
	kfs    []keyframe
	out    []float64 // the last value worked out, returned by at
	last   int       // the keyframe at found last, where the next search starts
}

type keyframe struct {
	t    float64
	s, e []float64 // e is nil in newer files: the next keyframe's s
	hold bool
	// The easing curve, per dimension or one for all (nil: linear).
	ox, oy, ix, iy []float64
	// to and ti are a position's tangents: it moves along a curve.
	to, ti []float64
	arc    *arcTable
}

// at returns the property's value at frame f. The slice is the property's
// own: it changes on the next call.
func (p *prop) at(f float64) []float64 {
	if p == nil {
		return nil
	}
	k := p.kfs
	if len(k) == 0 {
		return p.static
	}
	if len(k) == 1 || f <= k[0].t {
		if k[0].s != nil {
			return k[0].s
		}
		return k[0].e
	}
	i := p.seek(f)
	if i >= len(k)-1 {
		return lastValue(k)
	}
	a, b := &k[i], &k[i+1]
	from, to := a.s, a.e
	if to == nil {
		to = b.s
	}
	if from == nil {
		from = to
	}
	if to == nil || a.hold || b.t <= a.t {
		return from
	}
	x := (f - a.t) / (b.t - a.t)
	n := min(len(from), len(to))
	if cap(p.out) < len(from) {
		p.out = make([]float64, len(from))
	}
	out := p.out[:n]
	if n >= 2 && a.to != nil {
		e := a.ease(0, x)
		q := a.arcPoint(from, to, e)
		out[0], out[1] = q.x, q.y
		for d := 2; d < n; d++ {
			out[d] = from[d] + (to[d]-from[d])*e
		}
		return out
	}
	for d := 0; d < n; d++ {
		out[d] = from[d] + (to[d]-from[d])*a.ease(d, x)
	}
	return out
}

// seek returns the last keyframe at or before f.
func (p *prop) seek(f float64) int {
	k := p.kfs
	i := min(p.last, len(k)-1)
	for i > 0 && k[i].t > f {
		i--
	}
	for i+1 < len(k) && k[i+1].t <= f {
		i++
	}
	p.last = i
	return i
}

func lastValue(k []keyframe) []float64 {
	l := &k[len(k)-1]
	if l.s != nil {
		return l.s
	}
	// Older files end with a keyframe holding only its time.
	if p := &k[len(k)-2]; p.e != nil {
		return p.e
	} else {
		return p.s
	}
}

func (k *keyframe) ease(d int, x float64) float64 {
	if k.ox == nil {
		return x
	}
	return cubicEase(pick(k.ox, d), pick(k.oy, d), pick(k.ix, d), pick(k.iy, d), x)
}

func pick(v []float64, d int) float64 {
	if d < len(v) {
		return v[d]
	}
	return v[0]
}

// cubicEase is CSS's cubic-bezier(x1, y1, x2, y2) at x.
func cubicEase(x1, y1, x2, y2, x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	if x1 == y1 && x2 == y2 {
		return x
	}
	x1 = math.Max(0, math.Min(1, x1))
	x2 = math.Max(0, math.Min(1, x2))
	t := x
	for range 8 {
		dx := bez1(t, x1, x2) - x
		if math.Abs(dx) < 1e-7 {
			return bez1(t, y1, y2)
		}
		d := bez1d(t, x1, x2)
		if math.Abs(d) < 1e-6 {
			break
		}
		t -= dx / d
		if t < 0 || t > 1 {
			break
		}
	}
	lo, hi := 0.0, 1.0
	t = x
	for range 40 {
		v := bez1(t, x1, x2)
		if math.Abs(v-x) < 1e-7 {
			break
		}
		if v < x {
			lo = t
		} else {
			hi = t
		}
		t = (lo + hi) / 2
	}
	return bez1(t, y1, y2)
}

// bez1 is a one-dimensional cubic from 0 through a and b to 1.
func bez1(t, a, b float64) float64 {
	u := 1 - t
	return 3*u*u*t*a + 3*u*t*t*b + t*t*t
}

func bez1d(t, a, b float64) float64 {
	u := 1 - t
	return 3*u*u*a + 6*u*t*(b-a) + 3*t*t*(1-b)
}

// arcSteps is how many pieces a position's curve is measured in.
const arcSteps = 64

// arcTable measures a position's curve between two keyframes, so it moves
// along it at the eased speed.
type arcTable struct {
	pts  [arcSteps + 1]vec
	lens [arcSteps + 1]float64
}

func (k *keyframe) arcPoint(from, to []float64, e float64) vec {
	if k.arc == nil {
		p0 := vec{from[0], from[1]}
		p3 := vec{to[0], to[1]}
		c1 := p0.add(vec{k.to[0], k.to[1]})
		c2 := p3.add(vec{k.ti[0], k.ti[1]})
		k.arc = new(arcTable)
		k.arc.pts[0] = p0
		for i := 1; i <= arcSteps; i++ {
			q := cubicAt(p0, c1, c2, p3, float64(i)/arcSteps)
			k.arc.pts[i] = q
			k.arc.lens[i] = k.arc.lens[i-1] + q.sub(k.arc.pts[i-1]).len()
		}
	}
	a := k.arc
	total := a.lens[arcSteps]
	switch {
	case e <= 0:
		return a.pts[0]
	case e >= 1:
		return a.pts[arcSteps]
	case total <= 0:
		return lerpv(a.pts[0], a.pts[arcSteps], e)
	}
	want := e * total
	lo, hi := 0, arcSteps
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if a.lens[mid] < want {
			lo = mid
		} else {
			hi = mid
		}
	}
	seg := a.lens[hi] - a.lens[lo]
	if seg <= 0 {
		return a.pts[lo]
	}
	return lerpv(a.pts[lo], a.pts[hi], (want-a.lens[lo])/seg)
}

// pathProp is an animatable path.
type pathProp struct {
	static *bez
	kfs    []pathKF
	out    bez
	last   int
}

// bez is a path as Lottie keeps it: vertices with in and out tangents
// relative to them.
type bez struct {
	v, i, o []vec
	closed  bool
}

type pathKF struct {
	t              float64
	s, e           *bez
	hold           bool
	ease           bool
	ox, oy, ix, iy float64
}

func (p *pathProp) at(f float64) *bez {
	if p == nil {
		return nil
	}
	k := p.kfs
	if len(k) == 0 {
		return p.static
	}
	if len(k) == 1 || f <= k[0].t {
		if k[0].s != nil {
			return k[0].s
		}
		return k[0].e
	}
	i := min(p.last, len(k)-1)
	for i > 0 && k[i].t > f {
		i--
	}
	for i+1 < len(k) && k[i+1].t <= f {
		i++
	}
	p.last = i
	if i >= len(k)-1 {
		if l := &k[len(k)-1]; l.s != nil {
			return l.s
		}
		if q := &k[len(k)-2]; q.e != nil {
			return q.e
		}
		return k[len(k)-2].s
	}
	a, b := &k[i], &k[i+1]
	from, to := a.s, a.e
	if to == nil {
		to = b.s
	}
	if from == nil {
		from = to
	}
	if to == nil || a.hold || b.t <= a.t {
		return from
	}
	x := (f - a.t) / (b.t - a.t)
	if a.ease {
		x = cubicEase(a.ox, a.oy, a.ix, a.iy, x)
	}
	n := min(len(from.v), len(to.v))
	o := &p.out
	o.v, o.i, o.o = grow(o.v, n), grow(o.i, n), grow(o.o, n)
	o.closed = from.closed
	for j := 0; j < n; j++ {
		o.v[j] = lerpv(from.v[j], to.v[j], x)
		o.i[j] = lerpv(from.i[j], to.i[j], x)
		o.o[j] = lerpv(from.o[j], to.o[j], x)
	}
	return o
}

func grow(v []vec, n int) []vec {
	if cap(v) < n {
		return make([]vec, n)
	}
	return v[:n]
}

// val1 returns a number property's value, or def.
func val1(p *prop, f, def float64) float64 {
	v := p.at(f)
	if len(v) == 0 {
		return def
	}
	return v[0]
}

// val2 returns a vector property's value, or (dx, dy).
func val2(p *prop, f, dx, dy float64) vec {
	v := p.at(f)
	switch len(v) {
	case 0:
		return vec{dx, dy}
	case 1:
		return vec{v[0], v[0]}
	}
	return vec{v[0], v[1]}
}
