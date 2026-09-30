// Package icon renders Material Symbols from SVG path data.
//
// Only the path commands Material Symbols actually use are supported:
// M, L, H, V, Q, T and Z (absolute and relative).
package icon

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"sync"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"golang.org/x/image/vector"
)

type seg struct {
	op   byte // 'M', 'L', 'Q' or 'Z'
	a, b f32.Point
}

// Icon is a parsed glyph with coordinates normalized to the unit square.
type Icon struct {
	segs []seg
}

// viewBox of Material Symbols: "0 -960 960 960".
const (
	vbSize = 960
	vbMinY = -960
)

func mustParse(d string) *Icon {
	ic, err := Parse(d, 0, vbMinY, vbSize)
	if err != nil {
		panic(err)
	}
	return ic
}

// Parse parses SVG path data whose square viewBox starts at (minX, minY).
func Parse(d string, minX, minY, size float32) (*Icon, error) {
	p := parser{s: d}
	ic := &Icon{}
	norm := func(pt f32.Point) f32.Point {
		return f32.Pt((pt.X-minX)/size, (pt.Y-minY)/size)
	}
	var cur, start, lastCtrl f32.Point
	var cmd byte
	prevQ := false
	for {
		p.skipSpace()
		if p.eof() {
			break
		}
		if c := p.s[p.i]; isCmd(c) {
			cmd = c
			p.i++
		} else if cmd == 0 {
			return nil, fmt.Errorf("icon: path data must start with a command")
		}
		rel := cmd >= 'a'
		base := f32.Point{}
		if rel {
			base = cur
		}
		isQ := false
		switch cmd | 0x20 { // lower-case
		case 'm':
			pt, err := p.point()
			if err != nil {
				return nil, err
			}
			cur = base.Add(pt)
			start = cur
			ic.segs = append(ic.segs, seg{op: 'M', a: norm(cur)})
			// Further coordinate pairs are implicit line-tos.
			if rel {
				cmd = 'l'
			} else {
				cmd = 'L'
			}
		case 'l':
			pt, err := p.point()
			if err != nil {
				return nil, err
			}
			cur = base.Add(pt)
			ic.segs = append(ic.segs, seg{op: 'L', a: norm(cur)})
		case 'h':
			x, err := p.number()
			if err != nil {
				return nil, err
			}
			cur = f32.Pt(base.X+x, cur.Y)
			ic.segs = append(ic.segs, seg{op: 'L', a: norm(cur)})
		case 'v':
			y, err := p.number()
			if err != nil {
				return nil, err
			}
			cur = f32.Pt(cur.X, base.Y+y)
			ic.segs = append(ic.segs, seg{op: 'L', a: norm(cur)})
		case 'q':
			c, err := p.point()
			if err != nil {
				return nil, err
			}
			e, err := p.point()
			if err != nil {
				return nil, err
			}
			ctrl := base.Add(c)
			cur = base.Add(e)
			ic.segs = append(ic.segs, seg{op: 'Q', a: norm(ctrl), b: norm(cur)})
			lastCtrl, isQ = ctrl, true
		case 't':
			e, err := p.point()
			if err != nil {
				return nil, err
			}
			ctrl := cur
			if prevQ {
				ctrl = cur.Mul(2).Sub(lastCtrl) // reflection of the previous control point
			}
			cur = base.Add(e)
			ic.segs = append(ic.segs, seg{op: 'Q', a: norm(ctrl), b: norm(cur)})
			lastCtrl, isQ = ctrl, true
		case 'z':
			ic.segs = append(ic.segs, seg{op: 'Z'})
			cur = start
		default:
			return nil, fmt.Errorf("icon: unsupported path command %q", cmd)
		}
		prevQ = isQ
	}
	return ic, nil
}

func isCmd(c byte) bool {
	switch c | 0x20 {
	case 'm', 'l', 'h', 'v', 'q', 't', 'z', 'c', 's', 'a':
		return true
	}
	return false
}

type parser struct {
	s string
	i int
}

func (p *parser) eof() bool { return p.i >= len(p.s) }

func (p *parser) skipSpace() {
	for !p.eof() && (p.s[p.i] == ' ' || p.s[p.i] == ',' || p.s[p.i] == '\n' || p.s[p.i] == '\t') {
		p.i++
	}
}

func (p *parser) number() (float32, error) {
	p.skipSpace()
	start := p.i
	if !p.eof() && (p.s[p.i] == '-' || p.s[p.i] == '+') {
		p.i++
	}
	dot := false
	for !p.eof() {
		c := p.s[p.i]
		switch {
		case c >= '0' && c <= '9':
		case c == '.' && !dot:
			dot = true
		case c == 'e' || c == 'E':
			p.i++
			if !p.eof() && (p.s[p.i] == '-' || p.s[p.i] == '+') {
				p.i++
			}
			continue
		default:
			goto done
		}
		p.i++
	}
done:
	v, err := strconv.ParseFloat(p.s[start:p.i], 32)
	if err != nil {
		return 0, fmt.Errorf("icon: bad number at %d: %w", start, err)
	}
	return float32(v), nil
}

func (p *parser) point() (f32.Point, error) {
	x, err := p.number()
	if err != nil {
		return f32.Point{}, err
	}
	y, err := p.number()
	return f32.Pt(x, y), err
}

// Path builds the icon outline scaled to size×size pixels at the origin.
func (ic *Icon) Path(ops *op.Ops, size float32) clip.PathSpec {
	var p clip.Path
	p.Begin(ops)
	open := false
	for _, s := range ic.segs {
		switch s.op {
		case 'M':
			if open {
				p.Close()
			}
			p.MoveTo(s.a.Mul(size))
			open = true
		case 'L':
			p.LineTo(s.a.Mul(size))
		case 'Q':
			p.QuadTo(s.a.Mul(size), s.b.Mul(size))
		case 'Z':
			p.Close()
			open = false
		}
	}
	return p.End()
}

// Fill paints the icon at the current offset.
func (ic *Icon) Fill(ops *op.Ops, size float32, col color.NRGBA) {
	paint.FillShape(ops, col, clip.Outline{Path: ic.Path(ops, size)}.Op())
}

// Layout paints the icon at size dp and returns its dimensions.
//
// Icons are rasterized once per size and color and drawn as images: that
// costs a few KB each, while drawing them as vector paths makes the GPU
// renderer keep path atlases that cost tens of MB.
func (ic *Icon) Layout(gtx layout.Context, size unit.Dp, col color.NRGBA) layout.Dimensions {
	px := gtx.Dp(size)
	img := ic.image(px, col)
	img.Add(gtx.Ops)
	defer clip.Rect{Max: image.Pt(px, px)}.Push(gtx.Ops).Pop()
	paint.PaintOp{}.Add(gtx.Ops)
	return layout.Dimensions{Size: image.Pt(px, px)}
}

type rasterKey struct {
	ic  *Icon
	px  int
	col color.NRGBA
}

var (
	rasterMu    sync.Mutex
	rasterCache = map[rasterKey]paint.ImageOp{}
)

func (ic *Icon) image(px int, col color.NRGBA) paint.ImageOp {
	k := rasterKey{ic, px, col}
	rasterMu.Lock()
	defer rasterMu.Unlock()
	if op, ok := rasterCache[k]; ok {
		return op
	}
	z := vector.NewRasterizer(px, px)
	ic.Rasterize(z, 0, 0, float32(px), 0)
	dst := image.NewRGBA(image.Rect(0, 0, px, px))
	z.Draw(dst, dst.Bounds(), image.NewUniform(col), image.Point{})
	op := paint.NewImageOp(dst)
	rasterCache[k] = op
	return op
}

// Rasterize adds the icon to z, scaled to size and rotated by angle
// (radians) around its center, with its top-left corner at (x, y).
func (ic *Icon) Rasterize(z *vector.Rasterizer, x, y, size, angle float32) {
	sin, cos := float32(math.Sin(float64(angle))), float32(math.Cos(float64(angle)))
	tr := func(p f32.Point) (float32, float32) {
		px, py := (p.X-0.5)*size, (p.Y-0.5)*size
		return x + size/2 + px*cos - py*sin, y + size/2 + px*sin + py*cos
	}
	open := false
	for _, s := range ic.segs {
		switch s.op {
		case 'M':
			if open {
				z.ClosePath()
			}
			z.MoveTo(tr(s.a))
			open = true
		case 'L':
			z.LineTo(tr(s.a))
		case 'Q':
			ax, ay := tr(s.a)
			bx, by := tr(s.b)
			z.QuadTo(ax, ay, bx, by)
		case 'Z':
			z.ClosePath()
			open = false
		}
	}
	if open {
		z.ClosePath()
	}
}
