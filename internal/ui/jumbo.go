package ui

import (
	"image"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// A text of one emoji and nothing else shows it big, without a bubble,
// with the time on a chip under it like a sticker's, as in WhatsApp. Two
// or three emojis alone stay in their bubble, bigger than text; so does
// one that has to be in one. Both animated emoji styles play them
// (emojistyle.go).
const jumboSize unit.Sp = 56

// bigEmojiSizes are the sizes of one, two and three emojis alone in a
// bubble.
var bigEmojiSizes = [jumboMax]unit.Sp{46, 38, 32}

// emojisAlone returns the emojis of a message that is nothing but one to
// jumboMax of them, or nil.
func emojisAlone(m *model.Message) []string {
	if m.Kind != model.KindText || m.Media != model.MediaNone ||
		m.Link != nil || m.Footer != "" || len(m.Buttons) > 0 || len(m.Whisper) > 0 ||
		m.Poll != nil || m.Event != nil || m.Location != nil || len(m.Contacts) > 0 || hasAttachment(m) ||
		m.Rich != nil || m.StatusMention != nil {
		return nil
	}
	return jumboEmojis(m.Text)
}

// jumbo returns the emoji m shows big without a bubble, alone in a slice,
// or nil when m is drawn in a bubble: WhatsApp does so for exactly one. A
// forward keeps the bubble; a reply's holds its quote alone, above the
// emoji.
func (u *UI) jumbo(c *model.Chat, m *model.Message) []string {
	if m.Forwarded || u.announcementsOf(c) != nil {
		return nil
	}
	if es := emojisAlone(m); len(es) == 1 {
		return es
	}
	return nil
}

// bigEmojiSize is the text size of m's bubble when it holds emojis alone,
// or 0.
func bigEmojiSize(m *model.Message) unit.Sp {
	if es := emojisAlone(m); len(es) > 0 {
		return bigEmojiSizes[len(es)-1]
	}
	return 0
}

// layoutJumboMessage draws m's emoji big, headed by the sender's name
// when it starts their run in a group, as a sticker is, or under the
// bubble of its quote when it's a reply.
func (u *UI) layoutJumboMessage(gtx C, c *model.Chat, m *model.Message, emojis []string, tail bool, maxW int) D {
	if m.Quote != nil {
		return u.layoutJumboReply(gtx, c, m, emojis, tail, maxW)
	}
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

// layoutJumboReply draws m's quote in a bubble, as a reply's text has it
// (under the sender's name), and the emoji below, without one.
func (u *UI) layoutJumboReply(gtx C, c *model.Chat, m *model.Message, emojis []string, tail bool, maxW int) D {
	p := u.pal
	out := m.FromMe
	bg, quoteBg, secondary := p.BubbleIn, p.QuoteIn, p.TextSecondary
	if out {
		bg, quoteBg, secondary = p.BubbleOut, p.QuoteOut, p.SecondaryOut
	}
	u.quoteClicked(gtx, m)
	pad := gtx.Dp(3)
	cgtx := gtx
	cgtx.Constraints = layout.Constraints{Max: image.Pt(max(0, maxW-2*pad), 1<<20)}
	qm := u.quotedMessage(m.Quote)
	w := min(cgtx.Constraints.Max.X, max(gtx.Dp(180), u.quoteWidth(cgtx, m.Quote, qm)))
	var sender part
	hasSender := c.IsGroup && !out && tail && m.Sender != ""
	if hasSender {
		sender = record(cgtx, func(gtx C) D { return u.layoutSenderName(gtx, m, p.BubbleIn) })
		w = min(cgtx.Constraints.Max.X, max(w, sender.size.X+gtx.Dp(12)))
	}
	quote := record(cgtx, func(gtx C) D { return u.layoutQuote(gtx, m.ChatID, m.Quote, quoteBg, secondary, w, qm) })
	y := pad
	if hasSender {
		y += sender.size.Y + gtx.Dp(6)
	}
	bub := image.Pt(w+2*pad, y+quote.size.Y+pad)
	j := record(gtx, func(gtx C) D { return u.layoutJumbo(gtx, m, emojis) })
	full := max(bub.X, j.size.X)
	bx, jx := 0, 0
	if out {
		bx, jx = full-bub.X, full-j.size.X
	}
	t := op.Offset(image.Pt(bx, 0)).Push(gtx.Ops)
	u.paintBubble(gtx, bub.X, bub.Y, bg, out, tail)
	if hasSender {
		at := image.Pt(pad+gtx.Dp(6), pad+gtx.Dp(3))
		sender.at(gtx, at.X, at.Y)
		u.senderButton(gtx, m, at, sender.size)
	}
	quote.at(gtx, pad, y)
	qt := op.Offset(image.Pt(pad, y)).Push(gtx.Ops)
	qg := gtx
	qg.Constraints = layout.Exact(quote.size)
	clickable(qg, u.btn("quote:"+m.ID), func(gtx C) D { return D{Size: quote.size} })
	qt.Pop()
	t.Pop()
	jy := bub.Y + gtx.Dp(4)
	j.at(gtx, jx, jy)
	return D{Size: image.Pt(full, jy+j.size.Y)}
}

// quoteWidth is how wide q's quote would be to show its name and its text
// on one line each.
func (u *UI) quoteWidth(gtx C, q *model.Quote, qm *model.Message) int {
	measure := func(size unit.Sp, s string, w font.Weight) int {
		gtx := gtx
		gtx.Constraints = layout.Constraints{Max: image.Pt(1<<20, 1<<20)}
		return record(gtx, u.label(size, s, u.pal.Text, labelOpts{weight: w, maxLines: 1}).Layout).size.X
	}
	name, txt := u.quoteTexts(q)
	w := measure(13.5, plainText(txt), font.Normal)
	if mediaIcon(q.Media) != nil {
		w += gtx.Dp(20)
	}
	w = max(w, measure(13, name, font.SemiBold)) + gtx.Dp(12)
	if qm != nil && qm.Kind == model.KindImage {
		return w + gtx.Dp(66)
	}
	return w + gtx.Dp(10)
}

// layoutJumbo draws emojis side by side, with the time on a chip under
// their right end.
func (u *UI) layoutJumbo(gtx C, m *model.Message, emojis []string) D {
	p := u.pal
	size := jumboSize
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
		for i, e := range emojis {
			t := op.Offset(image.Pt(i*cell.X, 0)).Push(gtx.Ops)
			if !u.layoutEmojiImage(gtx, e, px, cell, animateIn(emojiAlone)) {
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
