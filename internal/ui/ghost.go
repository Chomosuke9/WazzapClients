package ui

import (
	"image"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Ghost mode (/ghost) makes you invisible: the backend sends no read
// receipts and shows you offline (model.PrefGhost), and package auto
// refuses whatever you'd send. The UI shows a bar above the chat list and,
// where the composer was, a button that turns it off. Both come and go
// with an animation: the bar grows open, and the composer sinks away as
// the ghost bar rises into its place.

// ghostAnims are the animations of ghost mode coming and going.
type ghostAnims struct {
	bar     tween // the bar above the chat list
	compose tween // the composer turning into the ghost bar
}

// durGhost is how long ghost mode takes to come or go.
const durGhost = durPanel

// ghostMode reports whether ghost mode is on.
func (u *UI) ghostMode() bool { return u.auto != nil && u.auto.Ghost() }

// SetGhost turns ghost mode on or off (used for screenshots).
func (u *UI) SetGhost(on bool) { u.setGhost(on) }

// setGhost turns ghost mode on or off. Turning it off marks the open chat
// read, as opening it would have; other chats read meanwhile stay unread
// until they're opened again.
func (u *UI) setGhost(on bool) {
	setExtra(u.backend, model.PrefGhost, on)
	if on {
		u.endSelect()
		u.closePicker()
		return
	}
	if u.selected != nil {
		u.backend.Open(u.selected.ID)
	}
	u.requestFocus(&u.conv.composer)
}

// ghostOff turns ghost mode off from one of its buttons.
func (u *UI) ghostOff() {
	u.setGhost(false)
	u.toast("Ghost mode is off")
}

// layoutGhostBar shows, above the chat list, that ghost mode is on. It
// grows open from under the filter chips, fading and sliding in, and
// shrinks away when ghost mode ends, so the list below glides instead of
// jumping.
func (u *UI) layoutGhostBar(gtx C) D {
	on := u.ghostMode()
	v := u.ghostFx.bar.step(gtx, on, durGhost)
	if v == 0 {
		return D{}
	}
	cl := u.btn("ghost:off")
	bgtx := gtx
	if on {
		if cl.Clicked(gtx) {
			u.ghostOff()
		}
	} else {
		var pop func()
		bgtx, pop = fadeOut(gtx) // it's going: clicks go through
		defer pop()
	}
	m := record(bgtx, func(gtx C) D { return u.ghostBarBody(gtx, cl) })
	if v == 1 {
		m.at(gtx, 0, 0)
		return D{Size: m.size}
	}
	e := easeOut(v)
	h := lerpInt(0, m.size.Y, e)
	defer clip.Rect{Max: image.Pt(m.size.X, h)}.Push(gtx.Ops).Pop()
	fx := pushFx(gtx, e, moveBy(0, -float32(m.size.Y)*(1-e)*0.5))
	m.at(gtx, 0, 0)
	fx.Pop()
	return D{Size: image.Pt(m.size.X, h)}
}

func (u *UI) ghostBarBody(gtx C, cl *widget.Clickable) D {
	p := u.pal
	return layout.Inset{Left: 21, Right: 21, Bottom: 10}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return background(gtx, p.Banner, 12, func(gtx C) D {
			return layout.Inset{Left: 14, Right: 8, Top: 6, Bottom: 6}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(iconW(icVisibilityOff, 18, p.Green)),
					layout.Rigid(layout.Spacer{Width: 10}.Layout),
					layout.Flexed(1, u.label(14, "Ghost mode · invisible, read only", p.BannerText,
						labelOpts{maxLines: 1}).Layout),
					layout.Rigid(func(gtx C) D { return u.ghostButton(gtx, cl) }),
				)
			})
		})
	})
}

// layoutGhostSwap draws the composer's place while ghost mode comes or
// goes, at progress v (0 for base, what shows without ghost mode, 1 for
// the ghost bar): base sinks and fades away as the ghost bar rises and
// fades in, and the height between them glides from one to the other.
// Only the one arriving takes input.
func (u *UI) layoutGhostSwap(gtx C, v float32, on bool, base layout.Widget) D {
	if v >= 1 {
		return u.layoutGhostComposer(gtx, 1)
	}
	bgtx, ggtx := gtx, gtx
	var pop func()
	if on {
		bgtx, pop = fadeOut(gtx)
	} else {
		ggtx, pop = fadeOut(gtx)
	}
	mb := record(bgtx, base)
	mg := record(ggtx, func(gtx C) D { return u.layoutGhostComposer(gtx, v) })
	pop()
	// Staggered, so the two barely overlap: base is mostly gone before
	// the ghost bar shows.
	out, in := smooth(min(v/0.6, 1)), smooth(max((v-0.35)/0.65, 0))
	w, h := gtx.Constraints.Max.X, lerpInt(mb.size.Y, mg.size.Y, smooth(v))
	defer clip.Rect{Max: image.Pt(w, h)}.Push(gtx.Ops).Pop()
	if out < 1 {
		fx := pushFx(gtx, 1-out, moveBy(0, float32(h-mb.size.Y)+float32(mb.size.Y)*out*0.6))
		mb.at(gtx, 0, 0)
		fx.Pop()
	}
	if in > 0 {
		fx := pushFx(gtx, in, moveBy(0, float32(h-mg.size.Y)+float32(mg.size.Y)*(1-in)*0.6))
		mg.at(gtx, 0, 0)
		fx.Pop()
	}
	return D{Size: image.Pt(w, h)}
}

// layoutGhostComposer takes the composer's place while ghost mode is on.
// v is how far it has come in (see layoutGhostSwap): its eye pops in with
// a little overshoot.
func (u *UI) layoutGhostComposer(gtx C, v float32) D {
	p := u.pal
	cl := u.btn("ghost:compose")
	if cl.Clicked(gtx) {
		u.ghostOff()
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return background(gtx, p.Panel, 0, func(gtx C) D {
		gtx.Constraints.Min.Y = gtx.Dp(64)
		return layout.Center.Layout(gtx, func(gtx C) D {
			return layout.Inset{Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						s := gtx.Dp(20)
						fx := pushFx(gtx, 1, scaleAt(image.Pt(s/2, s/2), lerp(0.4, 1, easeOutBack(v))))
						d := iconW(icVisibilityOff, 20, p.Green)(gtx)
						fx.Pop()
						return d
					}),
					layout.Rigid(layout.Spacer{Width: 10}.Layout),
					layout.Rigid(u.label(15, "Ghost mode is on. You can't send messages.", p.TextSecondary,
						labelOpts{maxLines: 1}).Layout),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					layout.Rigid(func(gtx C) D { return u.ghostButton(gtx, cl) }),
				)
			})
		})
	})
}

// ghostButton is the "Turn off" button of the ghost bars.
func (u *UI) ghostButton(gtx C, cl *widget.Clickable) D {
	p := u.pal
	return clickable(gtx, cl, func(gtx C) D {
		return background(gtx, faded(p.Hover, u.hover(gtx, cl)), 16, func(gtx C) D {
			return layout.UniformInset(8).Layout(gtx,
				u.label(14, "Turn off", p.Green, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
		})
	})
}
