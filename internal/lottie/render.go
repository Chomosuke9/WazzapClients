package lottie

import (
	"image"
	"math"
)

// renderer draws frames. Its buffers are kept from frame to frame.
type renderer struct {
	frameRate float64
	w, h      int
	stamp     int

	pts []vec // the frame's paths (pathRef)
	cur int32 // the path being built

	top    layerBuf // dst, while it renders
	ras    raster
	stroke stroker
	dash   dasher
	poly   []vec
	coll   []xpath // the paths a paint draws
	targ   []*node // the shapes a modifier changes
	free   []*layerBuf
	cov    []float32 // a mask's coverage
	mask   []float32 // the masks so far
	lut    [256][4]float32

	segLens    [][lenSteps + 1]float64
	lens       []float64
	tv, to, ti []vec
	rv         []roundVtx
	geo        []pathRef
}

// xpath is a path with the transform to the space of the paint drawing it.
type xpath struct {
	ref pathRef
	m   mat
}

func (r *renderer) render(dst *image.RGBA, root *comp, f, cw, ch float64) {
	w, h := dst.Rect.Dx(), dst.Rect.Dy()
	if w <= 0 || h <= 0 {
		return
	}
	if r.w != w || r.h != h {
		r.w, r.h = w, h
		r.free = nil
		r.cov, r.mask = nil, nil
	}
	r.ras.reset(w, h)
	r.stamp++
	r.pts = r.pts[:0]

	var target *layerBuf
	if dst.Stride == w*4 {
		r.top = layerBuf{pix: dst.Pix[:w*h*4], w: w, h: h}
		target = &r.top
		clear(target.pix)
	} else {
		target = r.get()
	}
	s := math.Min(float64(w)/cw, float64(h)/ch)
	m := scaling(s, s).then(translate((float64(w)-cw*s)/2, (float64(h)-ch*s)/2))
	if root != nil {
		r.drawComp(root, f, m, target, 1)
		// lottie-web clips the animation to the composition.
		clipOutside(target, m.apply(vec{0, 0}), m.apply(vec{cw, ch}))
	}
	if dst.Stride != w*4 {
		for y := 0; y < h; y++ {
			copy(dst.Pix[y*dst.Stride:y*dst.Stride+w*4], target.pix[y*w*4:(y+1)*w*4])
		}
		r.put(target)
	}
	r.top.pix = nil // don't keep dst
}

// clipOutside clears b outside the rectangle from a to z, fading the
// pixels its edges cross.
func clipOutside(b *layerBuf, a, z vec) {
	d := b.dirty
	for y := d.Min.Y; y < d.Max.Y; y++ {
		ky := overlap(float64(y), a.y, z.y)
		for x := d.Min.X; x < d.Max.X; x++ {
			k := float32(ky * overlap(float64(x), a.x, z.x))
			if k >= 1 {
				continue
			}
			i := (y*b.w + x) * 4
			p := b.pix[i : i+4 : i+4]
			p[0] = u8(float32(p[0]) * k)
			p[1] = u8(float32(p[1]) * k)
			p[2] = u8(float32(p[2]) * k)
			p[3] = u8(float32(p[3]) * k)
		}
	}
}

// overlap returns how much of the pixel from v to v+1 lies between lo and
// hi.
func overlap(v, lo, hi float64) float64 {
	return math.Max(0, math.Min(v+1, hi)-math.Max(v, lo))
}

// get returns a cleared layer buffer.
func (r *renderer) get() *layerBuf {
	if n := len(r.free); n > 0 {
		b := r.free[n-1]
		r.free = r.free[:n-1]
		return b
	}
	return &layerBuf{pix: make([]uint8, r.w*r.h*4), w: r.w, h: r.h}
}

func (r *renderer) put(b *layerBuf) {
	b.clear()
	r.free = append(r.free, b)
}

func (r *renderer) drawComp(c *comp, f float64, m mat, dst *layerBuf, alpha float64) {
	for i := len(c.layers) - 1; i >= 0; i-- {
		l := c.layers[i]
		if l.matteSrc || l.hidden || f < l.ip || f >= l.op {
			continue
		}
		r.drawLayer(l, f, m, dst, alpha)
	}
}

// local returns the time a layer's properties are read at: frame f of its
// composition, as lottie-web and rlottie read keyframes (their times are
// the composition's). A precomp's layers play at its own time, less its
// start (precompTime).
func (l *layer) local(f float64) float64 { return f }

// precompTime returns the frame a precomp's layers play at: its time remap,
// or the time since its start, stretched.
func (r *renderer) precompTime(l *layer, f float64) float64 {
	if l.tm != nil {
		return val1(l.tm, f, 0) * r.frameRate / l.sr
	}
	return (f - l.st) / l.sr
}

// matrix returns a layer's transform at frame f of its composition, its
// parents' included.
func (r *renderer) matrix(l *layer, f float64) mat {
	if l.mStamp == r.stamp && l.mFrame == f {
		return l.m
	}
	m := l.tr.matrix(l.local(f))
	if l.parent != nil {
		m = m.then(r.matrix(l.parent, f))
	}
	l.mStamp, l.mFrame, l.m = r.stamp, f, m
	return m
}

func (r *renderer) drawLayer(l *layer, f float64, m mat, dst *layerBuf, alpha float64) {
	if l.ty == layerNull {
		return
	}
	lm := r.matrix(l, f).then(m)
	op := alpha * l.tr.opacity(l.local(f))
	if op <= 0 {
		return
	}
	clip := l.ty == layerPrecomp && r.needsClip(lm, l.w, l.h)
	blend := blendable(l.blend)
	if len(l.masks) == 0 && l.matte == nil && !clip && !blend && (l.ty != layerPrecomp || op >= 1) {
		r.drawContent(l, f, lm, dst, op)
		return
	}
	buf := r.get()
	r.drawContent(l, f, lm, buf, 1)
	if clip {
		r.clipRect(buf, lm, l.w, l.h)
	}
	if len(l.masks) > 0 {
		r.applyMasks(l, l.local(f), lm, buf)
	}
	if l.matte != nil {
		mb := r.get()
		if ml := l.matte; f >= ml.ip && f < ml.op {
			r.drawLayer(ml, f, m, mb, 1)
		}
		r.applyMatte(buf, mb, l.matteTy)
		r.put(mb)
	}
	if blend {
		dst.blend(buf, float32(op), l.blend)
	} else {
		dst.over(buf, float32(op))
	}
	r.put(buf)
}

func (r *renderer) drawContent(l *layer, f float64, lm mat, dst *layerBuf, alpha float64) {
	lf := l.local(f)
	switch l.ty {
	case layerShape:
		if l.root != nil {
			r.evalGroup(l.root, lf)
			r.drawGroup(l.root, lf, lm, dst, alpha)
		}
	case layerPrecomp:
		if l.comp == nil {
			return
		}
		r.drawComp(l.comp, r.precompTime(l, lf), lm, dst, alpha)
	case layerSolid:
		r.ras.reset(r.w, r.h)
		o := outliner{ras: &r.ras, m: lm}
		o.moveTo(vec{0, 0})
		o.lineTo(vec{l.w, 0})
		o.lineTo(vec{l.w, l.h})
		o.lineTo(vec{0, l.h})
		o.close()
		a := float32(alpha)
		sh := shader{r: float32(l.solid[0]) * a, g: float32(l.solid[1]) * a, b: float32(l.solid[2]) * a, a: a}
		r.fill(dst, false, &sh)
	case layerImage:
		if l.img != nil {
			r.drawImage(l.img, lm, dst, alpha)
		}
	}
}

// fill blends the raster's coverage onto dst.
func (r *renderer) fill(dst *layerBuf, evenOdd bool, sh *shader) {
	dst.touch(r.ras.bounds())
	r.ras.scan(evenOdd, func(y, x0 int, cov []float32) {
		dst.span(y, x0, cov, sh)
	})
}

// needsClip reports whether a precomp's bounds, through lm, leave out part
// of the frame.
func (r *renderer) needsClip(lm mat, w, h float64) bool {
	if w <= 0 || h <= 0 {
		return false
	}
	inv, ok := lm.invert()
	if !ok {
		return false
	}
	for _, c := range [4]vec{{0, 0}, {float64(r.w), 0}, {0, float64(r.h)}, {float64(r.w), float64(r.h)}} {
		q := inv.apply(c)
		if q.x < -0.01 || q.y < -0.01 || q.x > w+0.01 || q.y > h+0.01 {
			return true
		}
	}
	return false
}

// clipRect keeps what's inside the rectangle (0, 0, w, h) through lm.
func (r *renderer) clipRect(buf *layerBuf, lm mat, w, h float64) {
	if buf.dirty.Empty() {
		return
	}
	r.ras.reset(r.w, r.h)
	o := outliner{ras: &r.ras, m: lm}
	o.moveTo(vec{0, 0})
	o.lineTo(vec{w, 0})
	o.lineTo(vec{w, h})
	o.lineTo(vec{0, h})
	o.close()
	cov := r.coverage(buf.dirty, false)
	r.scaleBy(buf, cov)
}

// coverage rasterizes the raster's lines into r.cov over d (zero
// elsewhere in d).
func (r *renderer) coverage(d image.Rectangle, evenOdd bool) []float32 {
	if len(r.cov) != r.w*r.h {
		r.cov = make([]float32, r.w*r.h)
	}
	for y := d.Min.Y; y < d.Max.Y; y++ {
		clear(r.cov[y*r.w+d.Min.X : y*r.w+d.Max.X])
	}
	w := r.w
	r.ras.scan(evenOdd, func(y, x0 int, cov []float32) {
		row := r.cov[y*w:]
		for i, c := range cov {
			x := x0 + i
			if x >= d.Min.X && x < d.Max.X && y >= d.Min.Y && y < d.Max.Y {
				row[x] = c
			}
		}
	})
	return r.cov
}

// scaleBy multiplies buf over its dirty part by v (per pixel).
func (r *renderer) scaleBy(buf *layerBuf, v []float32) {
	d := buf.dirty
	for y := d.Min.Y; y < d.Max.Y; y++ {
		for x := d.Min.X; x < d.Max.X; x++ {
			k := v[y*r.w+x]
			i := (y*r.w + x) * 4
			if k >= 1 {
				continue
			}
			p := buf.pix[i : i+4 : i+4]
			if k <= 0 {
				p[0], p[1], p[2], p[3] = 0, 0, 0, 0
				continue
			}
			p[0] = u8(float32(p[0]) * k)
			p[1] = u8(float32(p[1]) * k)
			p[2] = u8(float32(p[2]) * k)
			p[3] = u8(float32(p[3]) * k)
		}
	}
}

func (r *renderer) applyMasks(l *layer, f float64, lm mat, buf *layerBuf) {
	d := buf.dirty
	if d.Empty() {
		return
	}
	if len(r.mask) != r.w*r.h {
		r.mask = make([]float32, r.w*r.h)
	}
	// As rlottie: a first mask that subtracts or intersects starts from
	// everything.
	start := float32(0)
	if m := l.masks[0].mode; m == 's' || m == 'i' {
		start = 1
	}
	for y := d.Min.Y; y < d.Max.Y; y++ {
		row := r.mask[y*r.w+d.Min.X : y*r.w+d.Max.X]
		for i := range row {
			row[i] = start
		}
	}
	for _, mk := range l.masks {
		z := mk.path.at(f)
		if z == nil {
			continue
		}
		r.ras.reset(r.w, r.h)
		mark := len(r.pts)
		if ref, ok := r.addBez(z); ok {
			r.fillPath(ref, lm)
		}
		r.pts = r.pts[:mark]
		cov := r.coverage(d, false)
		op := float32(clamp01(val1(mk.opacity, f, 100) / 100))
		for y := d.Min.Y; y < d.Max.Y; y++ {
			for x := d.Min.X; x < d.Max.X; x++ {
				i := y*r.w + x
				c := cov[i]
				if mk.inv {
					c = 1 - c
				}
				c *= op
				v := r.mask[i]
				switch mk.mode {
				case 'a':
					v = v + c - v*c
				case 's':
					v *= 1 - c
				case 'i':
					v *= c
				case 'l':
					v = max(v, c)
				case 'd':
					v = min(v, c)
				case 'f':
					v = float32(math.Abs(float64(v - c)))
				}
				r.mask[i] = v
			}
		}
	}
	r.scaleBy(buf, r.mask)
}

// applyMatte multiplies buf by the matte's alpha or luma.
func (r *renderer) applyMatte(buf, matte *layerBuf, ty int) {
	d := buf.dirty
	for y := d.Min.Y; y < d.Max.Y; y++ {
		for x := d.Min.X; x < d.Max.X; x++ {
			i := (y*r.w + x) * 4
			p := buf.pix[i : i+4 : i+4]
			if p[3] == 0 {
				continue
			}
			q := matte.pix[i : i+4 : i+4]
			var k float32
			switch ty {
			case 2:
				k = 1 - float32(q[3])/255
			case 3:
				k = (0.299*float32(q[0]) + 0.587*float32(q[1]) + 0.114*float32(q[2])) / 255
			case 4:
				k = 1 - (0.299*float32(q[0])+0.587*float32(q[1])+0.114*float32(q[2]))/255
			default:
				k = float32(q[3]) / 255
			}
			if k >= 1 {
				continue
			}
			p[0] = u8(float32(p[0]) * k)
			p[1] = u8(float32(p[1]) * k)
			p[2] = u8(float32(p[2]) * k)
			p[3] = u8(float32(p[3]) * k)
		}
	}
}

// evalGroup works out a group's paths and transforms at frame f, and
// applies its modifiers.
func (r *renderer) evalGroup(g *node, f float64) {
	for i, it := range g.items {
		switch it.kind {
		case kGroup:
			r.evalGroup(it, f)
			it.m = it.tr.matrix(f)
			it.alpha = it.tr.opacity(f)
		case kRepeater:
			r.evalGroup(it.content, f)
			it.n = int(math.Ceil(val1(it.copies, f, 0)))
			it.n = max(0, min(it.n, 1000))
		case kPath, kRect, kEllipse, kStar:
			it.geo = it.geo[:0]
			if ref, ok := r.shape(it, f); ok {
				it.geo = append(it.geo, ref)
			}
		case kTrim:
			r.trim(g.items[:i], it, f)
		case kRound:
			if rd := val1(it.round, f, 0); rd > 0 {
				r.targ = r.targets(r.targ[:0], g.items[:i])
				for _, t := range r.targ {
					for k, ref := range t.geo {
						t.geo[k] = r.roundCorners(ref, rd)
					}
				}
			}
		}
	}
}

// shape builds a shape's path at frame f.
func (r *renderer) shape(n *node, f float64) (pathRef, bool) {
	switch n.kind {
	case kPath:
		z := n.path.at(f)
		if z == nil {
			return pathRef{}, false
		}
		return r.addBez(z)
	case kRect:
		return r.addRect(val2(n.pos, f, 0, 0), val2(n.size, f, 0, 0), val1(n.round, f, 0), n.ccw), true
	case kEllipse:
		return r.addEllipse(val2(n.pos, f, 0, 0), val2(n.size, f, 0, 0), n.ccw), true
	case kStar:
		return r.addStar(val2(n.pos, f, 0, 0), val1(n.points, f, 5), val1(n.outerR, f, 0), val1(n.innerR, f, 0),
			val1(n.outerRnd, f, 0)/100, val1(n.innerRnd, f, 0)/100, val1(n.rot, f, 0), n.polygon, n.ccw)
	}
	return pathRef{}, false
}

// targets appends the shapes among items, groups' and repeaters' included.
func (r *renderer) targets(dst []*node, items []*node) []*node {
	for _, it := range items {
		switch {
		case it.kind.isShape():
			dst = append(dst, it)
		case it.kind == kGroup:
			dst = r.targets(dst, it.items)
		case it.kind == kRepeater:
			dst = r.targets(dst, it.content.items)
		}
	}
	return dst
}

// trim applies trim paths to the shapes among items, as lottie-web does.
func (r *renderer) trim(items []*node, t *node, f float64) {
	o := math.Mod(val1(t.offset, f, 0), 360) / 360
	if o < 0 {
		o++
	}
	s := clamp01(val1(t.start, f, 0)/100) + o
	e := clamp01(val1(t.end, f, 100)/100) + o
	if s > e {
		s, e = e, s
	}
	s = math.Round(s*10000) / 10000
	e = math.Round(e*10000) / 10000
	if s >= 1 && e >= 1 {
		s, e = s-1, e-1
	}
	if s == 0 && e == 1 {
		return
	}
	r.targ = r.targets(r.targ[:0], items)
	if s == e {
		for _, n := range r.targ {
			n.geo = n.geo[:0]
		}
		return
	}
	if !t.individually {
		for _, n := range r.targ {
			r.geo = r.geo[:0]
			for _, ref := range n.geo {
				l := r.pathLen(ref)
				r.trimOne(ref, s*l, e*l, l)
			}
			n.geo = append(n.geo[:0], r.geo...)
		}
		return
	}
	// Individually: one length through all the paths.
	r.lens = r.lens[:0]
	total := 0.0
	for _, n := range r.targ {
		for _, ref := range n.geo {
			l := r.pathLen(ref)
			r.lens = append(r.lens, l)
			total += l
		}
	}
	at, k := 0.0, 0
	for _, n := range r.targ {
		r.geo = r.geo[:0]
		for _, ref := range n.geo {
			l := r.lens[k]
			k++
			lo, hi := at, at+l
			at = hi
			// The window (s, e) of the total, and its wrapped part.
			for _, w := range [2][2]float64{{s * total, math.Min(e, 1) * total}, {0, math.Max(0, e-1) * total}} {
				a, b := math.Max(w[0], lo), math.Min(w[1], hi)
				if b > a {
					r.pathLen(ref)
					r.subPath(ref, a-lo, b-lo, true)
					r.geo = append(r.geo, r.end(false))
				}
			}
		}
		n.geo = append(n.geo[:0], r.geo...)
	}
}

// trimOne keeps the part of ref from a to b (b may pass its length l,
// going on from its start), adding the pieces to r.geo.
func (r *renderer) trimOne(ref pathRef, a, b, l float64) {
	if l <= 0 {
		return
	}
	if b <= l {
		r.subPath(ref, a, b, true)
		r.geo = append(r.geo, r.end(false))
		return
	}
	r.subPath(ref, a, l, true)
	if ref.closed {
		// One piece through the path's start.
		r.subPath(ref, 0, b-l, false)
		r.geo = append(r.geo, r.end(false))
		return
	}
	r.geo = append(r.geo, r.end(false))
	r.subPath(ref, 0, b-l, true)
	r.geo = append(r.geo, r.end(false))
}

// drawGroup draws a group's items, the last first (at the bottom). Each
// paint draws the paths of the items before it.
func (r *renderer) drawGroup(g *node, f float64, m mat, dst *layerBuf, alpha float64) {
	for i := len(g.items) - 1; i >= 0; i-- {
		it := g.items[i]
		switch {
		case it.kind.isPaint():
			r.coll = r.collect(r.coll[:0], g.items[:i], identity, f)
			if len(r.coll) > 0 {
				r.drawPaint(it, f, m, dst, alpha)
			}
		case it.kind == kGroup:
			if it.alpha > 0 {
				r.drawGroup(it, f, it.m.then(m), dst, alpha*it.alpha)
			}
		case it.kind == kRepeater:
			for c := 0; c < it.n; c++ {
				k := c
				if it.below {
					k = it.n - 1 - c
				}
				cm, ca := r.copyOf(it, k, f)
				if ca > 0 {
					r.drawGroup(it.content, f, cm.then(m), dst, alpha*ca)
				}
			}
		}
	}
}

// collect appends the paths of items, with the transforms to the space
// they're collected into.
func (r *renderer) collect(dst []xpath, items []*node, m mat, f float64) []xpath {
	for _, it := range items {
		switch {
		case it.kind.isShape():
			for _, ref := range it.geo {
				dst = append(dst, xpath{ref, m})
			}
		case it.kind == kGroup:
			dst = r.collect(dst, it.items, it.m.then(m), f)
		case it.kind == kRepeater:
			for c := 0; c < it.n; c++ {
				cm, _ := r.copyOf(it, c, f)
				dst = r.collect(dst, it.content.items, cm.then(m), f)
			}
		}
	}
	return dst
}

// copyOf returns copy k of a repeater's transform and opacity.
func (r *renderer) copyOf(rp *node, k int, f float64) (mat, float64) {
	t := rp.tr
	if t == nil {
		return identity, 1
	}
	n := float64(k) + val1(rp.offset, f, 0)
	a := val2(t.a, f, 0, 0)
	s := val2(t.s, f, 100, 100)
	p := val2(t.p, f, 0, 0)
	rot := val1(t.r, f, 0)
	m := translate(-a.x, -a.y).
		then(scaling(math.Pow(s.x/100, n), math.Pow(s.y/100, n))).
		then(rotation(rad(rot * n))).
		then(translate(a.x, a.y)).
		then(translate(p.x*n, p.y*n))
	so := val1(t.so, f, 100) / 100
	eo := val1(t.eo, f, 100) / 100
	op := so
	if rp.n > 1 {
		op = so + (eo-so)*float64(k)/float64(rp.n-1)
	}
	return m, clamp01(op)
}

// drawPaint fills or strokes r.coll.
func (r *renderer) drawPaint(p *node, f float64, m mat, dst *layerBuf, alpha float64) {
	op := alpha * clamp01(val1(p.opacity, f, 100)/100)
	if op <= 0 {
		return
	}
	var sh shader
	switch p.kind {
	case kFill, kStroke:
		c := p.color.at(f)
		cr, cg, cb, ca := color4(c)
		a := float32(op) * ca
		if a <= 0 {
			return
		}
		sh = shader{r: cr * a, g: cg * a, b: cb * a, a: a}
	default:
		if !r.gradient(&sh, p, f, m, op) {
			return
		}
	}
	r.ras.reset(r.w, r.h)
	stroke := p.kind == kStroke || p.kind == kGStroke
	if !stroke {
		for _, xp := range r.coll {
			r.fillPath(xp.ref, xp.m.then(m))
		}
		r.fill(dst, p.evenOdd, &sh)
		return
	}
	w := val1(p.width, f, 0)
	if !(w > 0) {
		return
	}
	scale := m.scale()
	st := &r.stroke
	st.o = outliner{ras: &r.ras, m: m}
	st.cap, st.join = p.lineCap, p.lineJoin
	if st.cap == 0 {
		st.cap = capRound
	}
	if st.join == 0 {
		st.join = joinRound
	}
	st.miter = p.miter
	if p.miterProp != nil {
		st.miter = val1(p.miterProp, f, 4)
	}
	if st.miter <= 0 {
		st.miter = 4
	}
	st.setup(w/2, scale)
	dashed := r.setupDashes(p, f)
	tol := flatTol / math.Max(scale, 1e-6)
	for _, xp := range r.coll {
		pts := r.flatten(xp.ref, xp.m, tol)
		if dashed {
			r.dash.cut(pts, xp.ref.closed, func(d []vec) { st.polyline(d, false) })
		} else {
			st.polyline(pts, xp.ref.closed)
		}
	}
	st.o.close()
	r.fill(dst, false, &sh)
}

func (r *renderer) setupDashes(p *node, f float64) bool {
	if len(p.dashes) == 0 {
		return false
	}
	d := &r.dash
	d.pattern = d.pattern[:0]
	d.offset = 0
	on := false
	for _, ds := range p.dashes {
		v := math.Max(0, val1(ds.v, f, 0))
		switch ds.kind {
		case 'o':
			d.offset = val1(ds.v, f, 0)
		default:
			d.pattern = append(d.pattern, v)
			if v > 0 {
				on = true
			}
		}
	}
	if len(d.pattern)%2 == 1 {
		d.pattern = append(d.pattern, d.pattern...)
	}
	return on
}

// color4 reads a color: 0..1, or 0..255 in some older files.
func color4(c []float64) (r, g, b, a float32) {
	if len(c) < 3 {
		return 0, 0, 0, 1
	}
	k := 1.0
	if c[0] > 1 || c[1] > 1 || c[2] > 1 {
		k = 1.0 / 255
	}
	a = 1
	if len(c) > 3 {
		a = float32(clamp01(c[3] * k))
	}
	return float32(clamp01(c[0] * k)), float32(clamp01(c[1] * k)), float32(clamp01(c[2] * k)), a
}

// gradient sets up a gradient's shader and color table.
func (r *renderer) gradient(sh *shader, p *node, f float64, m mat, op float64) bool {
	stops := p.gradStops.at(f)
	n := p.gradCount
	if n <= 0 || len(stops) < 4*n {
		return false
	}
	alphas := stops[4*n:]
	for i := 0; i < 256; i++ {
		t := float64(i) / 255
		cr, cg, cb := gradColor(stops[:4*n], t)
		a := 1.0
		if len(alphas) >= 2 {
			a = gradAlpha(alphas, t)
		}
		a *= op
		r.lut[i] = [4]float32{float32(cr * a), float32(cg * a), float32(cb * a), float32(a)}
	}
	sh.lut = &r.lut
	s := val2(p.gradStart, f, 0, 0)
	e := val2(p.gradEnd, f, 0, 0)
	inv, ok := m.invert()
	if !ok {
		return false
	}
	if p.gradType != 2 {
		d := e.sub(s)
		dd := d.dot(d)
		if dd == 0 {
			sh.kind = shadeLinear
			sh.ax, sh.ay, sh.a0 = 0, 0, 1
			return true
		}
		// t = (inv(P) - s)·d / dd, linear in the device point P.
		sh.kind = shadeLinear
		sh.ax = (inv.a*d.x + inv.b*d.y) / dd
		sh.ay = (inv.c*d.x + inv.d*d.y) / dd
		sh.a0 = ((inv.e-s.x)*d.x + (inv.f-s.y)*d.y) / dd
		return true
	}
	radius := e.sub(s).len()
	if radius == 0 {
		sh.kind = shadeLinear
		sh.ax, sh.ay, sh.a0 = 0, 0, 1
		return true
	}
	// The focal point, as lottie-web places it.
	hl := math.Max(-0.99, math.Min(0.99, val1(p.gradHL, f, 0)/100))
	ang := math.Atan2(e.y-s.y, e.x-s.x) + rad(val1(p.gradHA, f, 0))
	foc := vec{s.x + math.Cos(ang)*radius*hl, s.y + math.Sin(ang)*radius*hl}
	sh.kind = shadeRadial
	sh.inv = inv
	sh.foc = foc
	sh.d = s.sub(foc)
	sh.rad = radius
	sh.quad = sh.d.dot(sh.d) - radius*radius
	return true
}

func gradColor(st []float64, t float64) (r, g, b float64) {
	n := len(st) / 4
	if t <= st[0] {
		return st[1], st[2], st[3]
	}
	for i := 1; i < n; i++ {
		if t <= st[4*i] {
			a, b0 := st[4*(i-1):4*i], st[4*i:4*i+4]
			span := b0[0] - a[0]
			k := 0.0
			if span > 0 {
				k = (t - a[0]) / span
			}
			return a[1] + (b0[1]-a[1])*k, a[2] + (b0[2]-a[2])*k, a[3] + (b0[3]-a[3])*k
		}
	}
	l := st[4*(n-1):]
	return l[1], l[2], l[3]
}

func gradAlpha(st []float64, t float64) float64 {
	n := len(st) / 2
	if t <= st[0] {
		return st[1]
	}
	for i := 1; i < n; i++ {
		if t <= st[2*i] {
			a0, a1 := st[2*(i-1):2*i], st[2*i:2*i+2]
			span := a1[0] - a0[0]
			k := 0.0
			if span > 0 {
				k = (t - a0[0]) / span
			}
			return a0[1] + (a1[1]-a0[1])*k
		}
	}
	return st[2*(n-1)+1]
}

// drawImage draws an embedded picture, sampled bilinearly.
func (r *renderer) drawImage(im *imageAsset, lm mat, dst *layerBuf, alpha float64) {
	src := im.img
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	if sw == 0 || sh == 0 {
		return
	}
	// Layer space is the asset's size; the picture may be another.
	m := scaling(im.w/float64(sw), im.h/float64(sh)).then(lm)
	// Drawn at less than half its size: sample a halved copy.
	if det := math.Abs(m.a*m.d - m.b*m.c); det > 0 {
		if k := int(math.Floor(math.Log2(1 / math.Sqrt(det)))); k > 0 {
			src = im.level(k)
			m = scaling(float64(sw)/float64(src.Rect.Dx()), float64(sh)/float64(src.Rect.Dy())).then(m)
			sw, sh = src.Rect.Dx(), src.Rect.Dy()
		}
	}
	inv, ok := m.invert()
	if !ok {
		return
	}
	b := image.Rectangle{}
	for _, c := range [4]vec{{0, 0}, {float64(sw), 0}, {0, float64(sh)}, {float64(sw), float64(sh)}} {
		q := m.apply(c)
		pr := image.Rect(int(math.Floor(q.x)), int(math.Floor(q.y)), int(math.Ceil(q.x))+1, int(math.Ceil(q.y))+1)
		if b.Empty() {
			b = pr
		} else {
			b = b.Union(pr)
		}
	}
	b = b.Intersect(image.Rect(0, 0, r.w, r.h))
	if b.Empty() {
		return
	}
	dst.touch(b)
	a := float32(alpha)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			q := inv.apply(vec{float64(x) + 0.5, float64(y) + 0.5})
			cr, cg, cb, ca := sample(src, q.x-0.5, q.y-0.5)
			ca *= a
			if ca <= 0 {
				continue
			}
			i := (y*r.w + x) * 4
			p := dst.pix[i : i+4 : i+4]
			inv := 1 - ca
			p[0] = u8(cr*a + float32(p[0])*inv)
			p[1] = u8(cg*a + float32(p[1])*inv)
			p[2] = u8(cb*a + float32(p[2])*inv)
			p[3] = u8(ca*255 + float32(p[3])*inv)
		}
	}
}

// sample reads a premultiplied picture between pixels: colors 0..255,
// alpha 0..1. Outside it is transparent.
func sample(src *image.RGBA, x, y float64) (r, g, b, a float32) {
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := float32(x-float64(x0)), float32(y-float64(y0))
	w, h := src.Rect.Dx(), src.Rect.Dy()
	var acc [4]float32
	for dy := 0; dy < 2; dy++ {
		for dx := 0; dx < 2; dx++ {
			px, py := x0+dx, y0+dy
			if px < 0 || py < 0 || px >= w || py >= h {
				continue
			}
			k := (1 - fx) * (1 - fy)
			switch {
			case dx == 1 && dy == 0:
				k = fx * (1 - fy)
			case dx == 0 && dy == 1:
				k = (1 - fx) * fy
			case dx == 1 && dy == 1:
				k = fx * fy
			}
			i := py*src.Stride + px*4
			acc[0] += k * float32(src.Pix[i])
			acc[1] += k * float32(src.Pix[i+1])
			acc[2] += k * float32(src.Pix[i+2])
			acc[3] += k * float32(src.Pix[i+3])
		}
	}
	return acc[0], acc[1], acc[2], acc[3] / 255
}
