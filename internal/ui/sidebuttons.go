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

// Hovering a message shows buttons beside its bubble, on the side away
// from the bubble's edge of the screen, like WhatsApp: Forward, for media,
// links, documents and big emojis, and then React, which opens the
// reaction bar. They're bare icons on the wallpaper.
// Beside photos, videos, GIFs and documents (forwardStays), and beside an
// announcement, Forward is always there.

const (
	sideBtn    unit.Dp = 28 // a side button's size
	sideBtnGap unit.Dp = 8  // between the bubble and the nearest button
	sideBtnSep unit.Dp = 4  // between the buttons
)

// sideButtons says which side buttons a message gets, and whether they go
// before its bubble (to the left of your own messages) or after it.
type sideButtons struct {
	react, forward, before bool
	stays                  bool // Forward shows without a hover too
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
// room, the space beside its bubble, leaving out the one that shows only
// on hover first, or else Forward.
func (u *UI) sideButtonsFor(gtx C, c *model.Chat, m *model.Message, before bool, room int) sideButtons {
	gone := m.Kind == model.KindDeleted || m.Kind == model.KindUnsupported || !m.Revoked.IsZero()
	if gone {
		return sideButtons{}
	}
	sb := sideButtons{
		before: before,
		react: u.sendBlocked(c) == "" &&
			!(m.FromMe && (m.Receipt == model.Pending || m.Receipt == model.Failed)),
		forward: m.Kind != model.KindViewOnce && (forwardsBeside(m) || hasLinkCard(m) || u.jumbo(c, m) != nil),
	}
	sb.stays = sb.forward && forwardStays(m)
	if sb.width(gtx) > room {
		if sb.stays {
			sb.react = false
		} else {
			sb.forward = false
		}
	}
	if sb.width(gtx) > room {
		return sideButtons{before: before}
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

// forwardStays reports whether a message's Forward button is always there:
// beside a photo, a video, a GIF or a document. A sticker's, a link's and
// a big emoji's wait for the hover.
func forwardStays(m *model.Message) bool {
	if m.Kind == model.KindSticker {
		return false
	}
	switch m.Media {
	case model.MediaImage, model.MediaVideo, model.MediaGIF, model.MediaDocument:
		return true
	}
	return m.Kind == model.KindImage
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
// fading them in while show. Forward may be there all along (sb.stays).
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
	if v == 0 && !sb.stays {
		return
	}
	sz, sep := gtx.Dp(sideBtn), gtx.Dp(sideBtnSep)
	x, dir := bub.X+gtx.Dp(sideBtnGap), 1
	if sb.before {
		x, dir = -gtx.Dp(sideBtnGap)-sz, -1
	}
	// Forward goes nearest the bubble: React comes and goes beyond it.
	if sb.forward {
		fv := v
		if sb.stays {
			fv = 1
		}
		fx := pushFx(gtx, fv, moveBy(float32(dir*gtx.Dp(6))*(fv-1), 0))
		t := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		u.forwardButton(gtx, m, bub.Y)
		t.Pop()
		fx.Pop()
		x += dir * (sz + sep)
	}
	if sb.react && v > 0 {
		fx := pushFx(gtx, v, moveBy(float32(dir*gtx.Dp(6))*(v-1), 0))
		t := op.Offset(image.Pt(x, (bub.Y-sz)/2)).Push(gtx.Ops)
		u.sideButton(gtx, react, icReact)
		t.Pop()
		fx.Pop()
	}
}

// sideButton draws a button beside a message: an icon alone, which gets
// stronger under the pointer.
func (u *UI) sideButton(gtx C, c *widget.Clickable, ic *icon.Icon) D {
	p := u.pal
	gtx.Constraints = layout.Exact(image.Pt(gtx.Dp(sideBtn), gtx.Dp(sideBtn)))
	return clickable(gtx, c, func(gtx C) D {
		sz := gtx.Dp(sideBtn)
		d := centerIn(gtx, sz, iconW(ic, 20, p.Icon))
		if h := u.hover(gtx, c); h > 0 {
			withOpacity(gtx, h, func() { centerIn(gtx, sz, iconW(ic, 20, p.IconStrong)) })
		}
		return d
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
	if m.isOpen() && (noFocus(&m.scrim).Clicked(gtx) || u.selected == nil || u.selected.ID != m.chatID) {
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
		focusTrap(gtx)
		noFocus(&m.scrim).Layout(sgtx, func(gtx C) D { return D{Size: gtx.Constraints.Max} })
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
