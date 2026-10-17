package ui

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/op"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// senderColor is the color of a group member's name, picked from their ID
// so it is the same on their messages, quotes of them and everywhere else
// (names change; the ID doesn't). The name is used only without an ID.
func (u *UI) senderColor(id, name string) color.NRGBA {
	p := u.pal
	if id == "" {
		id = name
	}
	return p.Senders[hashIndex(id, len(p.Senders))]
}

// layoutSenderName draws a group message's sender: their name in their
// color and, beside it, the label they gave themselves in the group, in a
// pill of that color, like WhatsApp. The label keeps its width and the name
// is cut short when both don't fit.
//
// Measured on WhatsApp for Android: the label's caps are 0.85 of the
// name's and share its baseline, and the pill reaches 14 dp above that
// baseline and 4 dp below it, 6 dp past the label's sides and 4 dp after
// the name (scaled here to the 13 sp name).
func (u *UI) layoutSenderName(gtx C, m *model.Message, bg color.NRGBA) D {
	col := u.senderColor(m.SenderID, m.Sender)
	nameStyle := u.label(13, m.Sender, col, labelOpts{weight: font.SemiBold, maxLines: 1})
	if m.SenderLabel == "" {
		return nameStyle.Layout(gtx)
	}
	labelCol, pillCol, rimCol := u.labelColors(col, bg)
	gap, padX := gtx.Dp(3), gtx.Dp(5.5)
	above, below := gtx.Dp(13), gtx.Dp(3.5)

	lgtx := gtx
	lgtx.Constraints.Min = image.Point{}
	lgtx.Constraints.Max.X = max(0, gtx.Constraints.Max.X/2-2*padX)
	lm := op.Record(gtx.Ops)
	ld := u.label(11, m.SenderLabel, labelCol, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout(lgtx)
	label := lm.Stop()
	pillW := ld.Size.X + 2*padX

	ngtx := gtx
	ngtx.Constraints.Min = image.Point{}
	ngtx.Constraints.Max.X = max(0, gtx.Constraints.Max.X-pillW-gap)
	nm := op.Record(gtx.Ops)
	nd := nameStyle.Layout(ngtx)
	name := nm.Stop()

	// Baselines, from the top of each.
	nb, lb := nd.Size.Y-nd.Baseline, ld.Size.Y-ld.Baseline
	base := max(nb, above)
	at := func(c op.CallOp, x, y int) {
		t := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
		c.Add(gtx.Ops)
		t.Pop()
	}
	at(name, 0, base-nb)
	x := nd.Size.X + gap
	pill := image.Rect(x, base-above, x+pillW, base+below)
	r := (above + below) / 2
	if rimCol != pillCol {
		rim := max(1, gtx.Dp(0.5))
		fillRRect(gtx, pill, r, rimCol)
		pill, r = pill.Inset(rim), r-rim
	}
	fillRRect(gtx, pill, r, pillCol)
	at(label, x+padX, base-lb)
	h := max(base-nb+nd.Size.Y, base+below)
	w := max(x+pillW, gtx.Constraints.Min.X)
	return D{Size: image.Pt(w, max(h, gtx.Constraints.Min.Y)), Baseline: max(h, gtx.Constraints.Min.Y) - base}
}

// labelColors are a member label's text, pill and pill edge colors for a
// sender color on a bubble of bg. On WhatsApp's dark theme the text is the
// name's color a little lighter, and the pill a deep shade of its hue (not
// the name's color seen through, which would come out grey): measured,
// about a fifth of the hue at full saturation and 55% value over the
// bubble, inside a hairline rim a touch lighter and greyer. On the light
// theme, where names are already deep, the text is the name's color and
// the pill a light wash of it, without a rim.
func (u *UI) labelColors(col, bg color.NRGBA) (text, pill, rim color.NRGBA) {
	if !u.dark {
		pill = mix(bg, col, 0.12)
		return col, pill, pill
	}
	pill = mix(bg, vivid(col, 0.55), 0.2)
	return mix(col, rgb(0xffffff), 0.18), pill, mix(pill, rgb(0xffffff), 0.06)
}

// vivid is c's hue at full saturation and value v (0-1).
func vivid(c color.NRGBA, v float32) color.NRGBA {
	r, g, b := float32(c.R), float32(c.G), float32(c.B)
	hi, lo := max(r, g, b), min(r, g, b)
	if hi == lo {
		return c
	}
	// Stretch the channels so the lowest is 0 and the highest v.
	f := func(x float32) uint8 { return uint8((x - lo) / (hi - lo) * v * 255) }
	return color.NRGBA{R: f(r), G: f(g), B: f(b), A: c.A}
}

// labelChanged gives the open chat's loaded messages of a member their new
// label.
func (u *UI) labelChanged(e model.LabelEvent) {
	if u.selected == nil || u.selected.ID != e.ChatID {
		return
	}
	for _, m := range u.msgs {
		if m.FromMe {
			continue
		}
		for _, id := range e.Members {
			if m.SenderID == id {
				m.SenderLabel = e.Label
			}
		}
	}
}
