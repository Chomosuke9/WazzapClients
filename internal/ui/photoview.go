package ui

import (
	"image"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
)

// A profile picture shown big over the window, like WhatsApp's: clicking
// a picture in the info panel opens it. It shows the preview the avatars
// use until the full picture (Backend.ProfilePhoto, up to 640 px) loads,
// which isn't kept on disk and leaves the image cache when it closes.

type picView struct {
	open     bool
	id, name string
	kind     avatarKind
	anim     tween
	// origin is the clicked picture, in content coordinates; the photo
	// grows out of it. Empty grows it from its own middle.
	origin image.Rectangle
}

func (v *picView) isOpen() bool { return v.open }

// photoFullPx is the side the full picture decodes at; WhatsApp's are
// 640 px.
const photoFullPx = 1024

// openPicView shows id's picture, growing out of origin.
func (u *UI) openPicView(id, name string, kind avatarKind, origin image.Rectangle) {
	if old := u.picView.id; old != "" && old != id {
		u.images.forget("pv:" + old)
	}
	u.picView = picView{open: true, id: id, name: name, kind: kind, origin: origin}
	u.requestFocus(nil)
}

func (u *UI) closePicView() { u.picView.open = false }

// layoutPicView draws the open picture over everything.
func (u *UI) layoutPicView(gtx C) {
	v := &u.picView
	if !v.open && v.anim.v == 0 {
		return
	}
	p := u.pal
	if u.btn("pv:close").Clicked(gtx) || u.btn("pv:bg").Clicked(gtx) {
		v.open = false
	}
	a := v.anim.step(gtx, v.open, durSlide)
	if a == 0 && !v.open {
		u.images.forget("pv:" + v.id)
		*v = picView{}
		return
	}
	if !v.open {
		var done func()
		gtx, done = fadeOut(gtx)
		defer done()
	}
	sz := gtx.Constraints.Max
	e := easeOut(a)
	// As in the media viewer, the backdrop and the header fade and the
	// picture grows: an opacity layer the size of the window would stay.
	fillRect(gtx, image.Rectangle{Max: sz}, faded(p.Viewer, e))
	if v.open {
		// Clicking around the picture closes it, and nothing underneath.
		u.btn("pv:bg").Layout(gtx, func(gtx C) D { return D{Size: sz} })
	}

	top := pushFx(gtx, e, moveBy(0, -float32(gtx.Dp(12))*(1-e)))
	func() {
		t := op.Offset(image.Pt(gtx.Dp(29), 0)).Push(gtx.Ops)
		defer t.Pop()
		hg := gtx
		hg.Constraints = layout.Constraints{Max: image.Pt(sz.X/2, gtx.Dp(62))}
		vcenter(hg, gtx.Dp(62), func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return u.drawAvatar(gtx, v.id, v.name, v.kind, 42) }),
				layout.Rigid(layout.Spacer{Width: 17}.Layout),
				layout.Flexed(1, u.label(15.5, v.name, p.Text, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
			)
		})
	}()
	func() {
		s := gtx.Dp(42)
		t := op.Offset(image.Pt(sz.X-gtx.Dp(40)-s, gtx.Dp(31)-s/2)).Push(gtx.Ops)
		defer t.Pop()
		cl := u.btn("pv:close")
		clickable(gtx, cl, func(gtx C) D {
			if h := u.hover(gtx, cl); h > 0 {
				fillCircle(gtx, image.Pt(s/2, s/2), s/2, faded(p.Hover, h))
			}
			return centerIn(gtx, s, iconW(icClose, 24, p.IconStrong))
		})
	}()
	top.Pop()

	area := image.Rect(gtx.Dp(40), gtx.Dp(84), sz.X-gtx.Dp(40), sz.Y-gtx.Dp(40))
	side := min(area.Dx(), area.Dy(), gtx.Dp(640))
	if side <= 0 {
		return
	}
	dst := image.Rectangle{Max: image.Pt(side, side)}.Add(area.Min.Add(image.Pt((area.Dx()-side)/2, (area.Dy()-side)/2)))
	radius := 0
	if a < 1 {
		origin := v.origin
		if origin.Empty() {
			inset := dst.Size().Div(8)
			origin = image.Rectangle{Min: dst.Min.Add(inset), Max: dst.Max.Sub(inset)}
		}
		dst = viewerRect(origin, dst, e)
		radius = int(float32(dst.Dx()) / 2 * (1 - e)) // the round avatar squares off
	}
	// Clicking the picture itself keeps it open.
	stop := op.Offset(dst.Min).Push(gtx.Ops)
	u.btn("pv:pic").Layout(gtx, func(gtx C) D { return D{Size: dst.Size()} })
	stop.Pop()
	if pic := u.picViewImage(); pic != nil {
		if radius > 0 {
			// While it grows: a path, which a still picture shouldn't
			// cost (its coverage texture never shrinks).
			defer clip.UniformRRect(dst, radius).Push(gtx.Ops).Pop()
		} else {
			defer clip.Rect(dst).Push(gtx.Ops).Pop()
		}
		paintCover(gtx, pic.op, pic.size, dst)
		return
	}
	// No picture (yet): the placeholder, as big.
	t := op.Offset(dst.Min).Push(gtx.Ops)
	u.drawAvatar(gtx, v.id, v.name, v.kind, unit.Dp(float32(dst.Dx())/gtx.Metric.PxPerDp))
	t.Pop()
}

// picViewImage is the picture to show: the full one once it loaded,
// the preview until then, or blurred in privacy mode.
func (u *UI) picViewImage() *imgEntry {
	id, b := u.picView.id, u.backend
	if u.blurred() {
		if e := u.images.getBlurred("a:"+id, func() []byte { return b.Avatar(id) }); e.state == imgReady {
			return e
		}
		return nil
	}
	if e := u.images.get("pv:"+id, photoFullPx, func() []byte { return b.ProfilePhoto(id) }); e.state == imgReady {
		return e
	}
	return u.avatarImage(id)
}
