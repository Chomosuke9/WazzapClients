package ui

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

type (
	C = layout.Context
	D = layout.Dimensions
)

func fillRect(gtx C, r image.Rectangle, col color.NRGBA) {
	paint.FillShape(gtx.Ops, col, clip.Rect(r).Op())
}

func fillRRect(gtx C, r image.Rectangle, radius int, col color.NRGBA) {
	paint.FillShape(gtx.Ops, col, clip.UniformRRect(r, radius).Op(gtx.Ops))
}

func fillCircle(gtx C, center image.Point, radius int, col color.NRGBA) {
	r := image.Rect(center.X-radius, center.Y-radius, center.X+radius, center.Y+radius)
	paint.FillShape(gtx.Ops, col, clip.Ellipse(r).Op(gtx.Ops))
}

// borderRRect draws a rounded rectangle with a 1px border.
func borderRRect(gtx C, r image.Rectangle, radius int, bg, border color.NRGBA) {
	fillRRect(gtx, r, radius, border)
	fillRRect(gtx, r.Inset(1), max(radius-1, 0), bg)
}

// background paints col behind w, filling at least the incoming minimum size.
func background(gtx C, col color.NRGBA, radius unit.Dp, w layout.Widget) D {
	return layout.Background{}.Layout(gtx, func(gtx C) D {
		sz := gtx.Constraints.Min
		fillRRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(radius), col)
		return D{Size: sz}
	}, w)
}

// fill paints the whole constrained area and returns it as its size.
func fill(gtx C, col color.NRGBA) D {
	sz := gtx.Constraints.Max
	fillRect(gtx, image.Rectangle{Max: sz}, col)
	return D{Size: sz}
}

type labelOpts struct {
	weight   font.Weight
	maxLines int
	align    text.Alignment
	italic   bool
}

func (u *UI) label(size unit.Sp, txt string, col color.NRGBA, o ...labelOpts) material.LabelStyle {
	l := material.Label(u.th, size, txt)
	l.Color = col
	l.MaxLines = 1
	if len(o) > 0 {
		l.Font.Weight = o[0].weight
		l.MaxLines = o[0].maxLines
		l.Alignment = o[0].align
		if o[0].italic {
			l.Font.Style = font.Italic
		}
	}
	return l
}

// drawIcon paints ic at size dp at the current offset.
func drawIcon(gtx C, ic *icon.Icon, size unit.Dp, col color.NRGBA) D {
	return ic.Layout(gtx, size, col)
}

// iconW adapts drawIcon to a layout.Widget.
func iconW(ic *icon.Icon, size unit.Dp, col color.NRGBA) layout.Widget {
	return func(gtx C) D { return ic.Layout(gtx, size, col) }
}

// vcenter lays out w vertically centered in a box at least h px tall,
// keeping the incoming minimum width. Flex passes its cross-axis minimum to
// every child, so giving a row a minimum height would stretch its labels
// and pin their text to the top; this is the way to make a row taller.
func vcenter(gtx C, h int, w layout.Widget) D {
	minX := gtx.Constraints.Min.X
	gtx.Constraints.Min.Y = max(gtx.Constraints.Min.Y, h)
	return layout.W.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min = image.Pt(minX, 0)
		return w(gtx)
	})
}

// centerIn draws w centered in a box of size×size px.
func centerIn(gtx C, size int, w layout.Widget) D {
	gtx.Constraints = layout.Exact(image.Pt(size, size))
	return layout.Center.Layout(gtx, w)
}

// clickable wraps w in a Clickable and shows the hand cursor over it.
func clickable(gtx C, c *widget.Clickable, w layout.Widget) D {
	return c.Layout(gtx, func(gtx C) D {
		dims := w(gtx)
		defer clip.Rect{Max: dims.Size}.Push(gtx.Ops).Pop()
		pointer.CursorPointer.Add(gtx.Ops)
		return dims
	})
}

// iconButton is a round, hover-highlighted icon button of size box dp.
func (u *UI) iconButton(gtx C, c *widget.Clickable, ic *icon.Icon, box, size unit.Dp, col color.NRGBA) D {
	return clickable(gtx, c, func(gtx C) D {
		sz := gtx.Dp(box)
		if c.Hovered() {
			fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, u.pal.Hover)
		}
		return centerIn(gtx, sz, iconW(ic, size, col))
	})
}

// badge draws the unread-count pill.
func (u *UI) badge(gtx C, n int) D {
	h := gtx.Dp(21)
	gtx.Constraints.Min = image.Point{}
	m := op.Record(gtx.Ops)
	ld := u.label(12, itoa(n), u.pal.OnGreen, labelOpts{weight: font.Bold, maxLines: 1}).Layout(gtx)
	call := m.Stop()
	w := max(h, ld.Size.X+gtx.Dp(12))
	fillRRect(gtx, image.Rect(0, 0, w, h), h/2, u.pal.Green)
	t := op.Offset(image.Pt((w-ld.Size.X)/2, (h-ld.Size.Y)/2)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	return D{Size: image.Pt(w, h)}
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// strokeArc strokes a circular arc centered at c.
func strokeArc(gtx C, c f32.Point, r, start, sweep, width float32, col color.NRGBA) {
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(c.X+r*float32(math.Cos(float64(start))), c.Y+r*float32(math.Sin(float64(start)))))
	p.ArcTo(c, c, sweep)
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: width}.Op())
}

// statusIcon is the "Status" glyph: a ring inside a ring broken into four arcs.
func statusIcon(gtx C, size unit.Dp, col color.NRGBA) D {
	px := float32(gtx.Dp(size))
	c := f32.Pt(px/2, px/2)
	w := px * 0.09
	const gap = 0.42 // radians
	for i := 0; i < 4; i++ {
		start := float32(i)*math.Pi/2 - math.Pi/4 + gap/2
		strokeArc(gtx, c, px*0.42, start, math.Pi/2-gap, w, col)
	}
	strokeArc(gtx, c, px*0.22, 0, 2*math.Pi, w, col)
	return D{Size: image.Pt(int(px), int(px))}
}

// channelsIcon is the "Channels" glyph: a round speech bubble with a
// broadcast symbol inside.
func channelsIcon(gtx C, size unit.Dp, col color.NRGBA) D {
	px := float32(gtx.Dp(size))
	c := f32.Pt(px*0.52, px*0.47)
	r := px * 0.38
	w := px * 0.085
	pt := func(deg float64) f32.Point {
		a := deg * math.Pi / 180
		return f32.Pt(c.X+r*float32(math.Cos(a)), c.Y+r*float32(math.Sin(a)))
	}
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(pt(150))
	p.ArcTo(c, c, float32(310*math.Pi/180))
	p.LineTo(f32.Pt(px*0.1, px*0.9))
	p.Close()
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: w}.Op())
	fillCircle(gtx, image.Pt(int(c.X), int(c.Y)), int(px*0.06), col)
	strokeArc(gtx, c, px*0.18, -math.Pi/4, math.Pi/2, w, col)
	strokeArc(gtx, c, px*0.18, math.Pi*3/4, math.Pi/2, w, col)
	return D{Size: image.Pt(int(px), int(px))}
}

// chatsIcon is the filled "Chats" glyph: a message box with its tail at the
// top left and two text lines cut out.
func chatsIcon(gtx C, size unit.Dp, col, bg color.NRGBA) D {
	px := float32(gtx.Dp(size))
	u := px / 24
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(1.2*u, 4*u))
	p.LineTo(f32.Pt(19*u, 4*u))
	p.QuadTo(f32.Pt(22*u, 4*u), f32.Pt(22*u, 7*u))
	p.LineTo(f32.Pt(22*u, 17*u))
	p.QuadTo(f32.Pt(22*u, 20*u), f32.Pt(19*u, 20*u))
	p.LineTo(f32.Pt(8*u, 20*u))
	p.QuadTo(f32.Pt(5*u, 20*u), f32.Pt(5*u, 17*u))
	p.LineTo(f32.Pt(5*u, 8*u))
	p.Close()
	paint.FillShape(gtx.Ops, col, clip.Outline{Path: p.End()}.Op())
	line := func(x0, x1, y float32) {
		fillRRect(gtx, image.Rect(int(x0*u), int((y-1)*u), int(x1*u), int((y+1)*u)), int(u), bg)
	}
	line(9, 18, 10.5)
	line(9, 16, 14.5)
	return D{Size: image.Pt(int(px), int(px))}
}

// rotated draws w rotated by angle radians around the center of a size×size box.
func rotated(gtx C, size int, angle float32, w layout.Widget) D {
	c := f32.Pt(float32(size)/2, float32(size)/2)
	defer op.Affine(f32.AffineId().Rotate(c, angle)).Push(gtx.Ops).Pop()
	return centerIn(gtx, size, w)
}
