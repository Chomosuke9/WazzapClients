package ui

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// Hovering a message shows round buttons beside its bubble, on the side
// away from the bubble's edge of the screen, like WhatsApp: React, which
// opens the reaction bar, and for media, links and documents Forward.
// An announcement's Forward is always there instead.

const (
	sideBtn    unit.Dp = 28 // a side button's size
	sideBtnGap unit.Dp = 8  // between the bubble and the nearest button
	sideBtnSep unit.Dp = 4  // between the buttons
)

// sideButtons says which side buttons a message gets, and whether they go
// before its bubble (to the left of your own messages) or after it.
type sideButtons struct {
	react, forward, before bool
}

// sideHover registers the hover area of m's bubble of size bub, drawn at
// the current offset, x from the left of a row w wide with margin beside
// it, and reports whether it is hovered. With buttons set, the area takes
// in the side buttons that fit, so the pointer can get to them, and they
// are returned for layoutSideButtons.
func (u *UI) sideHover(gtx C, c *model.Chat, m *model.Message, bub image.Point, x, w, margin int, buttons bool) (bool, sideButtons) {
	var sb sideButtons
	if buttons {
		sb.before = m.FromMe
		room := w + margin - x - bub.X
		if sb.before {
			room = x + margin
		}
		sb = u.sideButtonsFor(gtx, c, m, sb.before, room)
	}
	area := image.Rectangle{Max: bub}
	if sb.before {
		area.Min.X -= sb.width(gtx)
	} else {
		area.Max.X += sb.width(gtx)
	}
	defer op.Offset(area.Min).Push(gtx.Ops).Pop()
	return u.hoverArea(gtx, m.ID, area.Size()), sb
}

// sideButtonsFor picks the side buttons of message m of chat c that fit in
// room, the space beside its bubble, leaving out Forward first.
func (u *UI) sideButtonsFor(gtx C, c *model.Chat, m *model.Message, before bool, room int) sideButtons {
	gone := m.Kind == model.KindDeleted || m.Kind == model.KindUnsupported || !m.Revoked.IsZero()
	if gone {
		return sideButtons{}
	}
	sb := sideButtons{
		before: before,
		react: !isChannelID(c.ID) && u.sendBlocked(c) == "" &&
			!(m.FromMe && (m.Receipt == model.Pending || m.Receipt == model.Failed)),
		forward: m.Kind != model.KindViewOnce && (forwardsBeside(m) || hasLinkCard(m)),
	}
	if sb.width(gtx) > room {
		sb.forward = false
	}
	if sb.width(gtx) > room {
		sb.react = false
	}
	return sb
}

// forwardsBeside reports whether a message's media gets a Forward button
// beside it.
func forwardsBeside(m *model.Message) bool {
	if m.Kind == model.KindImage || m.Kind == model.KindSticker {
		return true
	}
	switch m.Media {
	case model.MediaImage, model.MediaVideo, model.MediaGIF, model.MediaDocument, model.MediaSticker:
		return true
	}
	return false
}

func (sb sideButtons) any() bool { return sb.react || sb.forward }

// width is the strip the buttons take beside the bubble, the gap to it
// included.
func (sb sideButtons) width(gtx C) int {
	n := 0
	if sb.react {
		n++
	}
	if sb.forward {
		n++
	}
	if n == 0 {
		return 0
	}
	return gtx.Dp(sideBtnGap) + n*gtx.Dp(sideBtn) + (n-1)*gtx.Dp(sideBtnSep)
}

// sideHovered reports whether the pointer is on one of m's side buttons, which
// keep themselves shown.
func (u *UI) sideHovered(m *model.Message) bool {
	return u.btn("sreact:"+m.ID).Hovered() || u.btn("sfwd:"+m.ID).Hovered()
}

// layoutSideButtons draws m's side buttons beside its bubble of size bub,
// fading them in while show.
func (u *UI) layoutSideButtons(gtx C, m *model.Message, sb sideButtons, bub image.Point, show bool) {
	if !sb.any() {
		return
	}
	react := u.btn("sreact:" + m.ID)
	if react.Clicked(gtx) {
		u.openReactBar(m)
	}
	show = show || u.sideHovered(m) || (u.ctx.isOpen() && u.ctx.kind == ctxReact && u.ctx.msg == m)
	v := smooth(u.anims.fade(gtx, animKey{p: react, tag: tagShow}, show, durHoverIn, durHoverOut))
	if v == 0 {
		return
	}
	sz, sep := gtx.Dp(sideBtn), gtx.Dp(sideBtnSep)
	// React goes nearest the bubble.
	var btns []func(gtx C, h int)
	if sb.react {
		btns = append(btns, func(gtx C, h int) {
			defer op.Offset(image.Pt(0, (h-sz)/2)).Push(gtx.Ops).Pop()
			u.sideButton(gtx, react, icReact)
		})
	}
	if sb.forward {
		btns = append(btns, func(gtx C, h int) { u.forwardButton(gtx, m, h) })
	}
	x, dir := bub.X+gtx.Dp(sideBtnGap), 1
	if sb.before {
		x, dir = -gtx.Dp(sideBtnGap)-sz, -1
	}
	fx := pushFx(gtx, v, moveBy(float32(dir*gtx.Dp(6))*(v-1), 0))
	for _, b := range btns {
		t := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		b(gtx, bub.Y)
		t.Pop()
		x += dir * (sz + sep)
	}
	fx.Pop()
}

// sideButton draws a round button beside a message.
func (u *UI) sideButton(gtx C, c *widget.Clickable, ic *icon.Icon) D {
	p := u.pal
	gtx.Constraints = layout.Exact(image.Pt(gtx.Dp(sideBtn), gtx.Dp(sideBtn)))
	return clickable(gtx, c, func(gtx C) D {
		sz := gtx.Dp(sideBtn)
		// Room is left for light mode's 1px shadow, which the Clickable
		// would clip.
		r := sz/2 - 1
		ctr := image.Pt(sz/2, sz/2-1)
		if !u.dark {
			fillCircle(gtx, ctr.Add(image.Pt(0, 1)), r, argb(0x0b141a, 0x21))
		}
		fillCircle(gtx, ctr, r, mix(p.Menu, p.MenuHover, u.hover(gtx, c)))
		return centerIn(gtx, sz, iconW(ic, 20, p.Icon))
	})
}

// openReactBar opens m's reaction bar alone, above its React button (the
// pointer).
func (u *UI) openReactBar(m *model.Message) {
	u.ctx = ctxMenu{kind: ctxReact, chatID: m.ChatID, msg: m, at: u.mouse}
}

// layoutReactMenu draws the reaction bar its React button opened, above
// the button or, near the top, below it.
func (u *UI) layoutReactMenu(gtx C) {
	m := &u.ctx
	if m.isOpen() && (m.scrim.Clicked(gtx) || u.selected == nil || u.selected.ID != m.chatID) {
		u.closeMenu()
	}
	v := m.anim.step(gtx, m.isOpen(), popDur(m.isOpen()))
	if v == 0 && m.closing {
		u.ctx = ctxMenu{}
		return
	}
	if m.closing {
		var done func()
		gtx, done = fadeOut(gtx)
		defer done()
	} else {
		sgtx := gtx
		sgtx.Constraints = layout.Exact(gtx.Constraints.Max)
		m.scrim.Layout(sgtx, func(gtx C) D { return D{Size: gtx.Constraints.Max} })
	}
	// The bar is about 43dp tall; top is its bottom middle.
	off := gtx.Dp(sideBtn)/2 + gtx.Dp(6)
	top := image.Pt(m.at.X, m.at.Y-off)
	if top.Y < gtx.Dp(43+8) {
		top.Y = m.at.Y + off + gtx.Dp(43)
	}
	defer pushPopup(gtx, v, m.at).Pop()
	u.layoutReactionBar(gtx, m.msg, top)
}
