package ui

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/emojiset"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// A text of one to three emojis and nothing else shows them big, without a
// bubble, with the time on a chip under them like a sticker's, as in
// WhatsApp. The fewer the emojis, the bigger. The animated emoji styles
// play them (emojistyle.go).
var jumboSizes = [jumboMax]unit.Sp{56, 46, 38}

// jumbo returns the emojis m shows big, or nil when it is drawn in a
// bubble. A reply, a forward or anything besides the text keeps the bubble.
func (u *UI) jumbo(c *model.Chat, m *model.Message) []string {
	if m.Kind != model.KindText || m.Media != model.MediaNone || m.Quote != nil || m.Forwarded ||
		m.Link != nil || m.Footer != "" || len(m.Buttons) > 0 || len(m.Whisper) > 0 ||
		m.Poll != nil || m.Event != nil || m.Location != nil || len(m.Contacts) > 0 ||
		hasAttachment(m) || u.announcementsOf(c) != nil {
		return nil
	}
	return jumboEmojis(m.Text)
}

// layoutJumboMessage draws m's emojis big, headed by the sender's name
// when it starts their run in a group, as a sticker is.
func (u *UI) layoutJumboMessage(gtx C, c *model.Chat, m *model.Message, emojis []string, tail bool, maxW int) D {
	if !(c.IsGroup && !m.FromMe && tail && m.Sender != "") {
		return u.layoutJumbo(gtx, m, emojis)
	}
	j := record(gtx, func(gtx C) D { return u.layoutJumbo(gtx, m, emojis) })
	head := u.stickerHeader(gtx, m, j.size.X, maxW)
	head.at(gtx, 0, 0)
	x, y := stickerUnderHeader(gtx, head, j.size.X)
	j.at(gtx, x, y)
	return D{Size: image.Pt(max(head.size.X, x+j.size.X), y+j.size.Y)}
}

// layoutJumbo draws emojis side by side, with the time on a chip under
// their right end.
func (u *UI) layoutJumbo(gtx C, m *model.Message, emojis []string) D {
	p := u.pal
	size := jumboSizes[len(emojis)-1]
	px := gtx.Sp(size)
	// The size of an emoji's picture at that text size (Gio scales bitmap
	// glyphs by px/ppem).
	cell := image.Pt(px*notoWidth/notoPpem, px*128/notoPpem)
	bg, metaCol := p.BubbleIn, p.MetaIn
	if m.FromMe {
		bg, metaCol = p.BubbleOut, p.MetaOut
	}
	meta := record(gtx, func(gtx C) D {
		return u.card(gtx, 8, bg, func(gtx C) D {
			return layout.Inset{Left: 6, Right: 6, Top: 2, Bottom: 3}.Layout(gtx, func(gtx C) D {
				return u.layoutMeta(gtx, m, metaCol, nil)
			})
		})
	})
	ew := cell.X * len(emojis)
	w := max(ew, meta.size.X)
	ex := 0
	if m.FromMe {
		ex = w - ew // yours line up on the right
	}
	et := op.Offset(image.Pt(ex, 0)).Push(gtx.Ops)
	if hide := u.secret; hide < 1 {
		var fade paint.OpacityStack
		if hide > 0 {
			fade = paint.PushOpacity(gtx.Ops, 1-hide)
		}
		anim := animateJumbo()
		for i, e := range emojis {
			t := op.Offset(image.Pt(i*cell.X, 0)).Push(gtx.Ops)
			// An animation is square, as high as the still picture.
			if anim && u.drawEmojiAnim(gtx, emojiset.Name([]rune(e)), cell.Y, image.Pt((cell.X-cell.Y)/2, 0)) {
				t.Pop()
				continue
			}
			if !u.layoutEmojiImage(gtx, e, px, cell) {
				lg := gtx
				lg.Constraints = layout.Exact(cell)
				layout.Center.Layout(lg, u.label(size, e, p.Text).Layout)
			}
			t.Pop()
		}
		if hide > 0 {
			fade.Pop()
		}
	}
	if hide := u.secret; hide > 0 {
		redactBar(gtx, ew, cell.Y/2+px*7/20, px, p.Text, hide)
	}
	et.Pop()

	y := cell.Y + gtx.Dp(2)
	meta.at(gtx, w-meta.size.X, y)
	return D{Size: image.Pt(w, y+meta.size.Y)}
}
