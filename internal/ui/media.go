package ui

import (
	"bytes"
	"image"
	_ "image/jpeg" // thumbnails are JPEG
	_ "image/png"

	"gioui.org/f32"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// thumb decodes a message thumbnail once and caches the result (also
// failures, so a broken image isn't decoded every frame).
func (u *UI) thumb(m *model.Message) thumb {
	if len(m.Thumb) == 0 {
		return thumb{}
	}
	if t, ok := u.thumbs[m.ID]; ok {
		return t
	}
	var t thumb
	if img, _, err := image.Decode(bytes.NewReader(m.Thumb)); err == nil {
		t = thumb{op: paint.NewImageOp(img), size: img.Bounds().Size(), ok: true}
	}
	u.thumbs[m.ID] = t
	return t
}

// layoutImage draws a photo preview in r: the real thumbnail when there is
// one, the demo gradient, or a neutral placeholder.
func (u *UI) layoutImage(gtx C, r image.Rectangle, m *model.Message) {
	t := u.thumb(m)
	switch {
	case t.ok:
		defer clip.UniformRRect(r, gtx.Dp(6)).Push(gtx.Ops).Pop()
		// Scale to cover r, centered, like CSS object-fit: cover.
		sx := float32(r.Dx()) / float32(t.size.X)
		sy := float32(r.Dy()) / float32(t.size.Y)
		s := max(sx, sy)
		off := f32.Pt(
			float32(r.Min.X)+(float32(r.Dx())-float32(t.size.X)*s)/2,
			float32(r.Min.Y)+(float32(r.Dy())-float32(t.size.Y)*s)/2,
		)
		tr := op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(s, s)).Offset(off)).Push(gtx.Ops)
		t.op.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		tr.Pop()
	case m.ImageA != 0 || m.ImageB != 0:
		u.gradientImage(gtx, r, m.ImageA, m.ImageB)
	default:
		fillRRect(gtx, r, gtx.Dp(6), u.pal.RowSelected)
		isz := gtx.Dp(48)
		c := r.Min.Add(r.Size().Div(2))
		t := op.Offset(c.Sub(image.Pt(isz/2, isz/2))).Push(gtx.Ops)
		u.icons.layout(gtx, icCamera, 48, u.pal.TextSecondary)
		t.Pop()
	}
}
