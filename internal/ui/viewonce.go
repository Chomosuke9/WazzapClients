package ui

import (
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/desktop"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// View once messages (model.KindViewOnce) show as a line in their bubble
// that opens them: a photo or video in the viewer, alone and without its
// strip, forward or save, a voice message in place. Each opens once, and
// the window can't be captured while one is shown. Replay view once
// (Ethically gray features) opens them as often as you like and lets
// screenshots through.

// viewOnceLabel is what a view once message's bubble and previews say.
func viewOnceLabel(m *model.Message) string {
	switch {
	case m.Opened:
		return "Opened"
	case m.Media == model.MediaImage:
		return "Photo"
	case m.Media == model.MediaVideo:
		return "Video"
	case m.Media == model.MediaVoice:
		return "Voice message"
	}
	return "View once message"
}

// canOpenViewOnce reports whether a view once message opens here. Your
// own don't, as in WhatsApp.
func (u *UI) canOpenViewOnce(m *model.Message) bool {
	if m.OnPhone || m.Media == model.MediaNone {
		return false
	}
	return u.viewOnceReplay || !m.Opened && !m.FromMe
}

// openViewOnce opens a view once message clicked in its bubble.
func (u *UI) openViewOnce(m *model.Message) {
	playing := m.Media == model.MediaVoice && u.voice.key == fileKey(m)
	switch {
	case playing:
		u.toggleVoice(m) // pauses, or plays on, what was opened
		return
	case m.OnPhone || m.Media == model.MediaNone:
		u.toast("For added privacy, WhatsApp sends view once messages only to your phone. Open it there.")
		return
	case !u.canOpenViewOnce(m):
		if m.FromMe {
			u.toast("For added privacy, you can't open view once messages you send.")
		} else {
			u.toast("You opened this view once message already.")
		}
		return
	case m.Media == model.MediaVoice:
		u.toggleVoice(m)
	default:
		u.openViewer(m)
		u.viewer.items, u.viewer.viewOnce = []*model.Message{m}, true
	}
	if !u.viewOnceReplay && !m.FromMe && !m.Opened {
		u.backend.OpenedViewOnce(m)
	}
}

// viewOnceText is a view once bubble's text: its label, or how far its
// voice message has played.
func (u *UI) viewOnceText(gtx C, m *model.Message) string {
	if m.Media == model.MediaVoice && u.voice.key == fileKey(m) {
		_, pos, playing, loading := u.voiceProgress(m)
		switch {
		case loading:
			gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(250 * time.Millisecond)})
			return "Loading…"
		case playing:
			gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(250 * time.Millisecond)})
			return "Playing · " + clock(pos)
		case u.voice.player != nil:
			return "Paused · " + clock(pos)
		}
	}
	return viewOnceLabel(m)
}

// viewOnceMark is the view once badge as an icon, size dp square.
func (u *UI) viewOnceMark(gtx C, size unit.Dp, col color.NRGBA) D {
	return u.viewOnceRingIcon(gtx, size, col, true)
}

// viewOnceRingIcon is the view once ring, size dp square, around a 1
// unless one is false (an opened message's).
func (u *UI) viewOnceRingIcon(gtx C, size unit.Dp, col color.NRGBA, one bool) D {
	s := gtx.Dp(size)
	sp := unit.Sp(float32(size) * 0.48)
	if !one {
		sp = 0
	}
	viewOnceRing(gtx, u, image.Pt(s/2, s/2), s*2/5, col, sp)
	return D{Size: image.Pt(s, s)}
}

// layoutViewOnceCard draws an unopened view once message as WhatsApp
// does: a card with the badge and what it holds, which opens it, and
// room in its lower right for the bubble's meta (metaSize). The card is
// w wide, or as narrow as it may be when w is 0, and a shade lighter
// than the bubble (bg) in the dark theme, darker in the light one.
func (u *UI) layoutViewOnceCard(gtx C, m *model.Message, w int, metaSize image.Point, bg, secondary color.NRGBA) D {
	p := u.pal
	if u.dark {
		bg = mix(bg, rgb(0xffffff), 0.1)
	} else {
		bg = mix(bg, rgb(0x000000), 0.05)
	}
	mark := p.Green
	if !u.canOpenViewOnce(m) {
		mark = secondary
	}
	pad, ring, gap := gtx.Dp(10), gtx.Dp(24), gtx.Dp(7)
	lgtx := gtx
	lgtx.Constraints.Min = image.Point{}
	lgtx.Constraints.Max.X = max(0, gtx.Constraints.Max.X-2*pad-ring-gap)
	label := record(lgtx, u.label(15.7, u.viewOnceText(gtx, m), secondary, labelOpts{maxLines: 1}).Layout)
	if w == 0 {
		w = max(gtx.Dp(150), 2*pad+ring+gap+label.size.X, metaSize.X+2*gtx.Dp(8))
		w = min(w, gtx.Constraints.Max.X)
	}
	rowH := max(ring, label.size.Y)
	h := gtx.Dp(9) + rowH + gtx.Dp(10) + metaSize.Y + gtx.Dp(5)
	card := image.Rect(0, 0, w, h)
	btn := u.btn("vo:" + m.ID)
	cg := gtx
	cg.Constraints = layout.Exact(card.Size())
	clickable(cg, btn, func(gtx C) D {
		fillRRect(gtx, card, gtx.Dp(7), mix(bg, p.Text, 0.06*u.hover(gtx, btn)))
		return D{Size: card.Size()}
	})
	top := gtx.Dp(9)
	t := op.Offset(image.Pt(pad-gtx.Dp(3), top+(rowH-ring)/2)).Push(gtx.Ops)
	u.viewOnceRingIcon(gtx, 24, mark, true)
	t.Pop()
	label.at(gtx, pad-gtx.Dp(3)+ring+gap, top+(rowH-label.size.Y)/2)
	return D{Size: card.Size()}
}

// viewOnceRing draws WhatsApp's view once badge: a circle, solid round
// its left and dashed round its right, around a 1 of size sp (none if 0).
func viewOnceRing(gtx C, u *UI, mid image.Point, r int, col color.NRGBA, sp unit.Sp) {
	c := f32.Pt(float32(mid.X), float32(mid.Y))
	w := max(1, float32(r)*0.16)
	// A solid arc from just right of the bottom, round the left, to just
	// right of the top, then three dashes down the right.
	const start, sweep, dash = math.Pi * 0.45, math.Pi * 1.1, math.Pi * 0.12
	strokeArc(gtx, c, float32(r), start, sweep, w, col)
	gap := float32(2*math.Pi-sweep-3*dash) / 4
	for i := range 3 {
		a := start + sweep + gap + float32(i)*(dash+gap)
		strokeArc(gtx, c, float32(r), a, dash, w, col)
	}
	if sp == 0 {
		return
	}
	one := record(gtx, func(gtx C) D {
		gtx.Constraints.Min = image.Point{}
		return u.label(sp, "1", col, labelOpts{weight: font.Bold}).Layout(gtx)
	})
	one.at(gtx, mid.X-one.size.X/2, mid.Y-one.size.Y/2)
}

// blockCapture keeps the window out of screenshots and screen recordings
// while on is set (Windows), for a view once message being shown.
func (u *UI) blockCapture(on bool) {
	if on == u.captureBlocked {
		return
	}
	u.captureBlocked = on
	if u.host != nil && u.host.hwnd != 0 {
		desktop.BlockCapture(u.host.hwnd, on)
	}
}
