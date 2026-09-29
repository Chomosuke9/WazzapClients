package ui

import (
	"image"
	"image/color"
	"math"
	"strings"
	"unicode"

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
)

type (
	C = layout.Context
	D = layout.Dimensions
)

// iconCache keeps one rasterized widget.Icon per (icon, size, color). A single
// widget.Icon only caches its last size/color, so sharing one between call
// sites with different colors would re-rasterize every frame.
type iconCache struct {
	m map[iconKey]*widget.Icon
}

type iconKey struct {
	data *byte
	size int
	col  color.NRGBA
}

func (ic *iconCache) layout(gtx C, data []byte, size unit.Dp, col color.NRGBA) D {
	px := gtx.Dp(size)
	k := iconKey{&data[0], px, col}
	if ic.m == nil {
		ic.m = make(map[iconKey]*widget.Icon)
	}
	w, ok := ic.m[k]
	if !ok {
		var err error
		w, err = widget.NewIcon(data)
		if err != nil {
			panic(err)
		}
		ic.m[k] = w
	}
	gtx.Constraints = layout.Exact(image.Pt(px, px))
	return w.Layout(gtx, col)
}

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
}

func (u *UI) label(size unit.Sp, txt string, col color.NRGBA, o ...labelOpts) material.LabelStyle {
	l := material.Label(u.th, size, txt)
	l.Color = col
	l.MaxLines = 1
	if len(o) > 0 {
		l.Font.Weight = o[0].weight
		l.MaxLines = o[0].maxLines
		l.Alignment = o[0].align
	}
	return l
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

// iconButton is a round, hover-highlighted icon button.
func (u *UI) iconButton(gtx C, c *widget.Clickable, data []byte, col color.NRGBA, active bool) D {
	return clickable(gtx, c, func(gtx C) D {
		sz := gtx.Dp(40)
		if active || c.Hovered() {
			bg := u.pal.Hover
			if active {
				bg = u.pal.RailActiveBg
			}
			fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, bg)
		}
		off := (sz - gtx.Dp(24)) / 2
		t := op.Offset(image.Pt(off, off)).Push(gtx.Ops)
		u.icons.layout(gtx, data, 24, col)
		t.Pop()
		return D{Size: image.Pt(sz, sz)}
	})
}

// avatar draws a round avatar with the contact's initials, or a group glyph.
func (u *UI) avatar(gtx C, name string, group bool, size unit.Dp) D {
	px := gtx.Dp(size)
	dims := D{Size: image.Pt(px, px)}
	if group {
		bg := rgb(0xdfe5e7)
		fg := rgb(0xffffff)
		if u.dark {
			bg, fg = rgb(0x6a7175), rgb(0xcfd4d6)
		}
		fillCircle(gtx, image.Pt(px/2, px/2), px/2, bg)
		isz := size * 0.6
		off := (px - gtx.Dp(isz)) / 2
		defer op.Offset(image.Pt(off, off)).Push(gtx.Ops).Pop()
		u.icons.layout(gtx, icGroup, isz, fg)
		return dims
	}
	col := rgb(avatarColors[hashIndex(name, len(avatarColors))])
	fillCircle(gtx, image.Pt(px/2, px/2), px/2, col)
	l := u.label(unit.Sp(float32(size)*0.38), initials(name), rgb(0xffffff), labelOpts{weight: font.SemiBold, maxLines: 1})
	gtx.Constraints = layout.Exact(dims.Size)
	layout.Center.Layout(gtx, l.Layout)
	return dims
}

func initials(name string) string {
	var out []rune
	for _, f := range strings.Fields(name) {
		r := []rune(f)[0]
		if unicode.IsLetter(r) {
			out = append(out, unicode.ToUpper(r))
		}
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "#"
	}
	return string(out)
}

// badge draws a green pill with a count.
func (u *UI) badge(gtx C, n int, bg color.NRGBA) D {
	txt := itoa(n)
	h := gtx.Dp(20)
	macro := op.Record(gtx.Ops)
	gtx.Constraints.Min = image.Point{}
	l := u.label(12, txt, u.pal.BadgeText, labelOpts{weight: font.SemiBold, maxLines: 1})
	ld := l.Layout(gtx)
	call := macro.Stop()
	w := max(h, ld.Size.X+gtx.Dp(12))
	fillRRect(gtx, image.Rect(0, 0, w, h), h/2, bg)
	defer op.Offset(image.Pt((w-ld.Size.X)/2, (h-ld.Size.Y)/2)).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
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

// statusIcon is WhatsApp's "Status" glyph: a broken ring with a dot inside.
func statusIcon(gtx C, size unit.Dp, col color.NRGBA) D {
	px := float32(gtx.Dp(size))
	c := f32.Pt(px/2, px/2)
	w := px * 0.085
	r := px*0.5 - w*1.5
	for i := 0; i < 4; i++ {
		start := float32(-math.Pi/2) + float32(i)*math.Pi/2 + 0.22
		strokeArc(gtx, c, r, start, math.Pi/2-0.44, w, col)
	}
	fillCircle(gtx, image.Pt(int(c.X), int(c.Y)), int(px*0.2), col)
	return D{Size: image.Pt(int(px), int(px))}
}

// channelsIcon approximates WhatsApp's "Channels" glyph: a dot with
// broadcast waves on both sides.
func channelsIcon(gtx C, size unit.Dp, col color.NRGBA) D {
	px := float32(gtx.Dp(size))
	c := f32.Pt(px/2, px/2)
	w := px * 0.085
	fillCircle(gtx, image.Pt(int(c.X), int(c.Y)), int(px*0.12), col)
	for _, r := range []float32{px * 0.26, px * 0.42} {
		strokeArc(gtx, c, r, -math.Pi/4, math.Pi/2, w, col)
		strokeArc(gtx, c, r, math.Pi*3/4, math.Pi/2, w, col)
	}
	return D{Size: image.Pt(int(px), int(px))}
}

// pinIcon draws a small thumbtack, tilted like WhatsApp's pinned-chat glyph.
func pinIcon(gtx C, size unit.Dp, col color.NRGBA) D {
	px := float32(gtx.Dp(size))
	defer op.Affine(f32.Affine2D{}.Rotate(f32.Pt(px/2, px/2), math.Pi/4)).Push(gtx.Ops).Pop()
	u := px / 16
	r := func(x0, y0, x1, y1 float32) image.Rectangle {
		return image.Rect(int(x0*u), int(y0*u), int(x1*u), int(y1*u))
	}
	fillRRect(gtx, r(5, 1, 11, 3), int(u), col)      // head
	fillRect(gtx, r(6, 3, 10, 8), col)               // body
	fillRRect(gtx, r(3.5, 8, 12.5, 10), int(u), col) // collar
	fillRect(gtx, r(7.4, 10, 8.6, 15), col)          // needle
	return D{Size: image.Pt(int(px), int(px))}
}
