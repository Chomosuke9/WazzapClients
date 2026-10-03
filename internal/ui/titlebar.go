package ui

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
)

// titleBarHeight matches WhatsApp Desktop's custom title bar.
const titleBarHeight = unit.Dp(41)

// layoutTitleBar draws the window caption: app name on the left, the
// minimize/maximize/close buttons on the right. The rest is a move area, so
// Windows handles dragging, snapping and double-click-to-maximize natively.
func (u *UI) layoutTitleBar(gtx C) D {
	p := u.pal
	h := gtx.Dp(titleBarHeight)
	w := gtx.Constraints.Max.X
	fillRect(gtx, image.Rect(0, 0, w, h), p.Frame)

	btnW := gtx.Dp(46)
	moveW := w - 3*btnW
	privacyBtn := u.conn.State.LoggedIn() && u.privacy.toggle
	if privacyBtn {
		moveW -= btnW
	}
	gtx.Constraints = layout.Exact(image.Pt(moveW, h))
	u.deco.LayoutMove(gtx, func(gtx C) D {
		return vcenter(gtx, h, func(gtx C) D {
			gtx.Constraints.Min.X = moveW
			return layout.Inset{Left: 15}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return centerIn(gtx, gtx.Dp(22), iconW(icChatsFill, 20, p.Green))
					}),
					layout.Rigid(layout.Spacer{Width: 10}.Layout),
					layout.Rigid(u.label(12.5, "WazzapClients", p.FrameText).Layout),
				)
			})
		})
	})

	buttons := []struct {
		action system.Action
		glyph  func(gtx C, col color.NRGBA)
		close  bool
	}{
		{system.ActionMinimize, glyphMinimize, false},
		{system.ActionMaximize, u.glyphMaximize, false},
		{system.ActionClose, glyphClose, true},
	}
	x := moveW
	if privacyBtn {
		t := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		u.layoutPrivacyButton(gtx, btnW, h)
		t.Pop()
		x += btnW
	}
	for i, b := range buttons {
		t := op.Offset(image.Pt(x+i*btnW, 0)).Push(gtx.Ops)
		c := u.deco.Clickable(b.action)
		u.captionButton(gtx, c, btnW, h, b.close, b.glyph)
		t.Pop()
	}
	return D{Size: image.Pt(w, h)}
}

func (u *UI) captionButton(gtx C, c *widget.Clickable, w, h int, isClose bool, glyph func(C, color.NRGBA)) {
	gtx.Constraints = layout.Exact(image.Pt(w, h))
	c.Layout(gtx, func(gtx C) D {
		a := u.hover(gtx, c)
		if a > 0 {
			bg := u.pal.Hover
			if isClose {
				bg = u.pal.CloseHover
			}
			fillRect(gtx, image.Rect(0, 0, w, h), faded(bg, a))
		}
		g := gtx.Dp(10)
		t := op.Offset(image.Pt((w-g)/2, (h-g)/2)).Push(gtx.Ops)
		glyph(gtx, u.pal.FrameText)
		if isClose {
			// Glyphs are cached per color: cross-fade to white.
			withOpacity(gtx, a, func() { glyph(gtx, rgb(0xffffff)) })
		}
		t.Pop()
		return D{Size: image.Pt(w, h)}
	})
}

// Caption glyphs are 10×10dp line drawings in the style of Windows 11.

func glyphMinimize(gtx C, col color.NRGBA) {
	g := gtx.Dp(10)
	fillRect(gtx, image.Rect(0, g/2, g, g/2+max(1, gtx.Dp(1))), col)
}

func (u *UI) glyphMaximize(gtx C, col color.NRGBA) {
	cachedGlyph(gtx, glyphKey{name: "maximize", px: gtx.Dp(10), col: col, flag: u.deco.Maximized},
		func(gtx C) D { u.drawGlyphMaximize(gtx, col); return D{} })
}

func (u *UI) drawGlyphMaximize(gtx C, col color.NRGBA) {
	g := float32(gtx.Dp(10))
	lw := float32(max(1, gtx.Dp(1)))
	if u.deco.Maximized {
		// Restore: two overlapping squares.
		strokeRect(gtx, 0, g*0.25, g*0.75, g, lw, col)
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(f32.Pt(g*0.25, g*0.25))
		p.LineTo(f32.Pt(g*0.25, 0))
		p.LineTo(f32.Pt(g, 0))
		p.LineTo(f32.Pt(g, g*0.75))
		p.LineTo(f32.Pt(g*0.75, g*0.75))
		paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: lw}.Op())
		return
	}
	strokeRect(gtx, 0, 0, g, g, lw, col)
}

func glyphClose(gtx C, col color.NRGBA) {
	cachedGlyph(gtx, glyphKey{name: "close", px: gtx.Dp(10), col: col},
		func(gtx C) D { drawGlyphClose(gtx, col); return D{} })
}

func drawGlyphClose(gtx C, col color.NRGBA) {
	g := float32(gtx.Dp(10))
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(0, 0))
	p.LineTo(f32.Pt(g, g))
	p.MoveTo(f32.Pt(g, 0))
	p.LineTo(f32.Pt(0, g))
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: float32(max(1, gtx.Dp(1)))}.Op())
}

func strokeRect(gtx C, x0, y0, x1, y1, width float32, col color.NRGBA) {
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(x0, y0))
	p.LineTo(f32.Pt(x1, y0))
	p.LineTo(f32.Pt(x1, y1))
	p.LineTo(f32.Pt(x0, y1))
	p.Close()
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: width}.Op())
}
