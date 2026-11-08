package lottie

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/draw"
	_ "image/jpeg" // embedded pictures
	_ "image/png"
	"math"
	"strconv"
	"strings"

	_ "golang.org/x/image/webp"
)

type nodeKind uint8

const (
	kGroup nodeKind = iota
	kPath
	kRect
	kEllipse
	kStar
	kFill
	kStroke
	kGFill
	kGStroke
	kTrim
	kRound
	kRepeater
)

func (k nodeKind) isShape() bool { return k >= kPath && k <= kStar }
func (k nodeKind) isPaint() bool { return k >= kFill && k <= kGStroke }

// node is an item of a shape layer.
type node struct {
	kind nodeKind

	// Groups: their items and transform. A repeater repeats content.
	items   []*node
	tr      *transform
	content *node

	path *pathProp // kPath
	// Shapes' position, size, roundness (rectangles) and direction; stars'.
	pos, size, round *prop
	ccw              bool
	polygon          bool
	points, rot      *prop
	outerR, innerR   *prop
	outerRnd         *prop
	innerRnd         *prop

	// Paints.
	color, opacity *prop
	width          *prop
	lineCap        int
	lineJoin       int
	miter          float64
	miterProp      *prop
	dashes         []dash
	evenOdd        bool
	gradType       int
	gradStart      *prop
	gradEnd        *prop
	gradHL, gradHA *prop
	gradStops      *prop
	gradCount      int

	// Modifiers: trim paths (start, end, offset, individually), round
	// corners (round), repeater (copies, offset, composite below).
	start, end, offset *prop
	individually       bool
	copies             *prop
	below              bool

	// The current frame's state.
	geo   []pathRef // a shape's paths, in its group's space
	m     mat       // a group's transform
	alpha float64   // a group's opacity
	n     int       // a repeater's copies
}

type dash struct {
	kind byte // 'd', 'g' or 'o'
	v    *prop
}

// transform is a layer's, a group's or a repeater's transform.
type transform struct {
	a, p, px, py, s, r, o, sk, sa *prop
	so, eo                        *prop
	rx, ry, or                    *prop // 3D layers
}

func newTransform(j *jsonTransform) *transform {
	if j == nil {
		return nil
	}
	t := &transform{a: j.A, p: j.P.p, px: j.P.x, py: j.P.y, s: j.S, r: j.R, o: j.O, sk: j.SK, sa: j.SA, so: j.SO, eo: j.EO,
		rx: j.RX, ry: j.RY, or: j.OR}
	if t.r == nil {
		t.r = j.RZ
	}
	return t
}

// matrix returns the transform at frame f: anchor, scale, skew, rotation,
// position, in that order.
func (t *transform) matrix(f float64) mat {
	if t == nil {
		return identity
	}
	a := val2(t.a, f, 0, 0)
	s := val2(t.s, f, 100, 100)
	m := translate(-a.x, -a.y).then(scaling(s.x/100, s.y/100))
	if sk := val1(t.sk, f, 0); sk != 0 {
		sa := rad(val1(t.sa, f, 0))
		m = m.then(rotation(sa)).then(shearX(-math.Tan(rad(sk)))).then(rotation(-sa))
	}
	if t.rx != nil || t.ry != nil || t.or != nil {
		m = m.then(t.rotation3D(f))
	} else if r := val1(t.r, f, 0); r != 0 {
		m = m.then(rotation(rad(r)))
	}
	var p vec
	if t.p != nil {
		p = val2(t.p, f, 0, 0)
	} else {
		p = vec{val1(t.px, f, 0), val1(t.py, f, 0)}
	}
	return m.then(translate(p.x, p.y))
}

// rotation3D turns a 3D layer as lottie-web does (rz, ry, rx, then the
// orientation's z, y and x) and projects it flat, without perspective.
func (t *transform) rotation3D(f float64) mat {
	var or [3]float64
	copy(or[:], t.or.at(f))
	r := m3z(rad(val1(t.r, f, 0)))
	r = m3y(rad(val1(t.ry, f, 0))).mul(r)
	r = m3x(rad(val1(t.rx, f, 0))).mul(r)
	r = m3z(rad(or[2])).mul(r)
	r = m3y(rad(or[1])).mul(r)
	r = m3x(rad(or[0])).mul(r)
	return mat{a: r[0][0], b: r[1][0], c: r[0][1], d: r[1][1]}
}

// m3 is a 3D rotation, acting on column vectors.
type m3 [3][3]float64

func (a m3) mul(b m3) m3 {
	var c m3
	for i := range 3 {
		for j := range 3 {
			c[i][j] = a[i][0]*b[0][j] + a[i][1]*b[1][j] + a[i][2]*b[2][j]
		}
	}
	return c
}

func m3z(a float64) m3 {
	s, c := math.Sincos(a)
	return m3{{c, -s, 0}, {s, c, 0}, {0, 0, 1}}
}

func m3y(a float64) m3 {
	s, c := math.Sincos(a)
	return m3{{c, 0, -s}, {0, 1, 0}, {s, 0, c}}
}

func m3x(a float64) m3 {
	s, c := math.Sincos(a)
	return m3{{1, 0, 0}, {0, c, s}, {0, -s, c}}
}

func (t *transform) opacity(f float64) float64 {
	if t == nil {
		return 1
	}
	return clamp01(val1(t.o, f, 100) / 100)
}

func clamp01(v float64) float64 {
	if !(v > 0) {
		return 0
	}
	return math.Min(v, 1)
}

const (
	layerPrecomp = 0
	layerSolid   = 1
	layerImage   = 2
	layerNull    = 3
	layerShape   = 4
)

type layer struct {
	ty       int
	ip, op   float64
	st, sr   float64
	tr       *transform
	parent   *layer
	hidden   bool
	root     *node       // shape layers
	comp     *comp       // precomps
	img      *imageAsset // image layers
	w, h     float64     // a precomp's or solid's size
	solid    [3]float64
	tm       *prop
	masks    []mask
	matteSrc bool   // td: a track matte, not drawn by itself
	matte    *layer // the layer whose alpha or luma mattes this one
	matteTy  int    // 1 alpha, 2 inverted alpha, 3 luma, 4 inverted luma
	blend    int    // blend mode (blendNormal...)

	// The matrix at the frame worked out last.
	mStamp int
	mFrame float64
	m      mat
}

type mask struct {
	mode    byte // 'a' add, 's' subtract, 'i' intersect, 'l', 'd', 'f'
	inv     bool
	path    *pathProp
	opacity *prop
}

type comp struct {
	layers []*layer
}

type imageAsset struct {
	img    *image.RGBA
	w, h   float64
	levels []*image.RGBA // img halved again and again, made as needed
}

// level returns the picture halved k times (box filtered), so that drawing
// it much smaller doesn't skip its pixels.
func (im *imageAsset) level(k int) *image.RGBA {
	if k <= 0 {
		return im.img
	}
	if len(im.levels) == 0 {
		im.levels = append(im.levels, im.img)
	}
	for len(im.levels) <= k {
		src := im.levels[len(im.levels)-1]
		sw, sh := src.Rect.Dx(), src.Rect.Dy()
		if sw <= 1 && sh <= 1 {
			return src
		}
		w, h := max(1, sw/2), max(1, sh/2)
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				var acc [4]int
				for dy := 0; dy < 2; dy++ {
					for dx := 0; dx < 2; dx++ {
						i := min(2*y+dy, sh-1)*src.Stride + min(2*x+dx, sw-1)*4
						for c := 0; c < 4; c++ {
							acc[c] += int(src.Pix[i+c])
						}
					}
				}
				j := y*dst.Stride + x*4
				for c := 0; c < 4; c++ {
					dst.Pix[j+c] = uint8((acc[c] + 2) / 4)
				}
			}
		}
		im.levels = append(im.levels, dst)
	}
	return im.levels[k]
}

// builder turns the JSON into layers and nodes.
type builder struct {
	assets map[string]*jsonAsset
	comps  map[string]*comp
	images map[string]*imageAsset
}

// maxDepth limits how deep precomps nest.
const maxDepth = 16

func (b *builder) comp(jl []jsonLayer, w, h float64, depth int) *comp {
	c := &comp{}
	byInd := map[int]*layer{}
	for i := range jl {
		j := &jl[i]
		l := &layer{
			ty: j.Ty, ip: j.IP, op: j.OP, st: j.ST, sr: j.SR,
			tr: newTransform(&j.KS), hidden: j.HD, tm: j.TM,
			matteSrc: j.TD != 0, matteTy: j.TT, blend: rawInt(j.BM),
		}
		if l.sr == 0 {
			l.sr = 1
		}
		switch j.Ty {
		case layerPrecomp:
			if depth < maxDepth {
				l.comp = b.precomp(idString(j.RefID), depth+1)
			}
			l.w, l.h = j.W, j.H
		case layerSolid:
			l.solid = hexColor(j.SC)
			l.w, l.h = j.SW, j.SH
		case layerImage:
			l.img = b.image(idString(j.RefID))
		case layerShape:
			l.root = buildGroup(j.Shapes)
		}
		for _, jm := range j.Masks {
			if jm.PT == nil || jm.Mode == "" || jm.Mode == "n" {
				continue
			}
			l.masks = append(l.masks, mask{mode: jm.Mode[0], inv: jm.Inv, path: jm.PT, opacity: jm.O})
		}
		c.layers = append(c.layers, l)
		if j.Ind != nil {
			byInd[*j.Ind] = l
		}
	}
	for i, l := range c.layers {
		if p := jl[i].Parent; p != nil {
			l.parent = byInd[*p]
		}
		if l.matteTy != 0 {
			if tp := jl[i].TP; tp != nil {
				l.matte = byInd[*tp]
			} else if i > 0 {
				l.matte = c.layers[i-1]
			}
			if l.matte == l {
				l.matte = nil
			}
		}
	}
	// Break parent loops.
	for _, l := range c.layers {
		p, n := l.parent, 0
		for p != nil && n <= len(c.layers) {
			p, n = p.parent, n+1
		}
		if p != nil {
			l.parent = nil
		}
	}
	return c
}

func (b *builder) precomp(id string, depth int) *comp {
	if c, ok := b.comps[id]; ok {
		return c
	}
	a := b.assets[id]
	if a == nil || a.Layers == nil {
		return nil
	}
	b.comps[id] = nil // a precomp holding itself draws nothing
	c := b.comp(a.Layers, a.W, a.H, depth)
	b.comps[id] = c
	return c
}

// image decodes a picture embedded in the file. Pictures it links to
// aren't loaded.
func (b *builder) image(id string) *imageAsset {
	if im, ok := b.images[id]; ok {
		return im
	}
	b.images[id] = nil
	a := b.assets[id]
	if a == nil || !strings.HasPrefix(a.P, "data:") {
		return nil
	}
	comma := strings.IndexByte(a.P, ',')
	if comma < 0 {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(a.P[comma+1:])
	if err != nil {
		return nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width*cfg.Height > 4096*4096 {
		return nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	bnd := src.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, bnd.Dx(), bnd.Dy()))
	draw.Draw(rgba, rgba.Bounds(), src, bnd.Min, draw.Src)
	im := &imageAsset{img: rgba, w: a.W, h: a.H}
	if im.w <= 0 || im.h <= 0 {
		im.w, im.h = float64(bnd.Dx()), float64(bnd.Dy())
	}
	b.images[id] = im
	return im
}

func hexColor(s string) [3]float64 {
	s = strings.TrimPrefix(s, "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil || len(s) != 6 {
		return [3]float64{}
	}
	return [3]float64{float64(v>>16&0xff) / 255, float64(v>>8&0xff) / 255, float64(v&0xff) / 255}
}

// buildGroup builds a group's items. A repeater takes the items before it.
func buildGroup(items []jsonShape) *node {
	g := &node{kind: kGroup}
	for i := range items {
		s := &items[i]
		if s.HD {
			continue
		}
		switch s.Ty {
		case "tr":
			g.tr = &transform{a: s.A, p: s.P, s: s.S, r: rawProp(s.R), o: s.O, sk: s.SK, sa: s.SA}
		case "rp":
			rp := &node{kind: kRepeater, copies: s.C, offset: s.O, below: s.M == 2, tr: newTransform(s.TR)}
			rp.content = &node{kind: kGroup, items: g.items}
			g.items = []*node{rp}
		default:
			if n := buildItem(s); n != nil {
				g.items = append(g.items, n)
			}
		}
	}
	return g
}

func buildItem(s *jsonShape) *node {
	switch s.Ty {
	case "gr":
		return buildGroup(s.It)
	case "sh":
		if s.KS == nil {
			return nil
		}
		return &node{kind: kPath, path: s.KS}
	case "rc":
		return &node{kind: kRect, pos: s.P, size: s.S, round: rawProp(s.R), ccw: rawInt(s.D) == 3}
	case "el":
		return &node{kind: kEllipse, pos: s.P, size: s.S, ccw: rawInt(s.D) == 3}
	case "sr":
		return &node{kind: kStar, pos: s.P, points: s.PT, rot: rawProp(s.R), outerR: s.OR, innerR: s.IR,
			outerRnd: s.OS, innerRnd: s.IS, polygon: s.SY == 2, ccw: rawInt(s.D) == 3}
	case "fl", "gf":
		n := &node{kind: kFill, color: s.C, opacity: s.O, evenOdd: rawInt(s.R) == 2}
		if s.Ty == "gf" {
			n.kind = kGFill
			gradient(n, s)
		}
		return n
	case "st", "gs":
		n := &node{kind: kStroke, color: s.C, opacity: s.O, width: s.W, lineCap: s.LC, lineJoin: s.LJ, miter: s.ML, miterProp: s.ML2}
		if s.Ty == "gs" {
			n.kind = kGStroke
			gradient(n, s)
		}
		var ds []jsonDash
		if len(s.D) > 0 && json.Unmarshal(s.D, &ds) == nil {
			for _, d := range ds {
				if d.V != nil && d.N != "" {
					n.dashes = append(n.dashes, dash{kind: d.N[0], v: d.V})
				}
			}
		}
		return n
	case "tm":
		return &node{kind: kTrim, start: s.S, end: s.E, offset: s.O, individually: s.M == 2}
	case "rd":
		return &node{kind: kRound, round: rawProp(s.R)}
	}
	return nil
}

func gradient(n *node, s *jsonShape) {
	n.gradType, n.gradStart, n.gradEnd, n.gradHL, n.gradHA = s.T, s.S, s.E, s.H, s.A
	if s.G != nil {
		n.gradStops, n.gradCount = &s.G.K, s.G.P
	}
}
