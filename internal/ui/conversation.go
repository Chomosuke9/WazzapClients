package ui

import (
	"fmt"
	"image"
	"image/color"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/styledtext"
)

type rowKind int

const (
	rowDate rowKind = iota
	rowEncryption
	rowMessage
)

// convRow is one entry of the message list: a day separator, the
// encryption notice, or a message.
type convRow struct {
	kind rowKind
	date string
	msg  *model.Message
	// first marks the first message of a run from the same sender. It gets
	// the bubble tail (and, in groups, the sender's name and avatar).
	first bool
}

// rows rebuilds the flattened message list when the loaded messages change.
func (u *UI) rows(c *model.Chat) []convRow {
	if u.conv.rowsFor == c && u.conv.rowsVer == u.msgsVer {
		return u.conv.rows
	}
	now := u.now()
	rows := []convRow{{kind: rowEncryption}}
	var prev *model.Message
	for _, m := range u.msgs {
		newDay := prev == nil || !sameDay(prev.Time, m.Time)
		if newDay {
			rows = append(rows, convRow{kind: rowDate, date: dateChip(m.Time, now)})
		}
		first := newDay || prev.FromMe != m.FromMe || prev.SenderID != m.SenderID ||
			m.Time.Sub(prev.Time) > 10*time.Minute
		rows = append(rows, convRow{kind: rowMessage, msg: m, first: first})
		prev = m
	}
	u.conv.rows, u.conv.rowsFor, u.conv.rowsVer = rows, c, u.msgsVer
	return rows
}

func (u *UI) layoutConversation(gtx C) D {
	c := u.selected
	var pinned *model.Message
	for _, m := range u.msgs {
		if m.Pinned && m.Kind != model.KindDeleted {
			pinned = m
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.layoutConvHeader(gtx, c) }),
		layout.Rigid(func(gtx C) D {
			if pinned == nil {
				return D{}
			}
			return u.layoutPinnedBanner(gtx, pinned)
		}),
		layout.Flexed(1, func(gtx C) D {
			sz := gtx.Constraints.Max
			u.conv.wallpaper.layout(gtx, u.pal.ChatBg, u.pal.Doodle)

			// The composer floats over the wallpaper; the list ends above it.
			m := op.Record(gtx.Ops)
			cgtx := gtx
			cgtx.Constraints = layout.Constraints{Min: image.Pt(sz.X, 0), Max: sz}
			var cd D
			if !isChannelID(c.ID) {
				// Channels are read-only.
				cd = u.layoutComposer(cgtx)
			}
			composer := m.Stop()

			lgtx := gtx
			lgtx.Constraints = layout.Exact(image.Pt(sz.X, max(0, sz.Y-cd.Size.Y)))
			u.layoutMessages(lgtx, c)

			t := op.Offset(image.Pt(0, sz.Y-cd.Size.Y)).Push(gtx.Ops)
			composer.Add(gtx.Ops)
			t.Pop()
			if u.picker.open && u.picker.mode == pickComposer {
				// Deferred so it draws (and takes clicks) above everything.
				m := op.Record(gtx.Ops)
				u.layoutPicker(gtx, image.Pt(gtx.Dp(12), sz.Y-cd.Size.Y+gtx.Dp(4)), sz.X-gtx.Dp(24))
				op.Defer(gtx.Ops, m.Stop())
			}
			return D{Size: sz}
		}),
	)
}

func (u *UI) layoutConvHeader(gtx C, c *model.Chat) D {
	p := u.pal
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return background(gtx, p.Panel, 0, func(gtx C) D {
		return vcenter(gtx, gtx.Dp(64), func(gtx C) D {
			return layout.Inset{Left: 17, Right: 16}.Layout(gtx, func(gtx C) D {
				sub := c.Presence
				if ch := u.channelByID(c.ID); ch != nil {
					sub = followers(ch.Followers)
				}
				if c.Typing != "" {
					sub = "typing…"
					if c.IsGroup {
						sub = shortName(c.Typing) + " is typing…"
					}
				}
				if sub == "" {
					sub = "click here for contact info"
					if c.IsGroup {
						sub = "click here for group info"
					}
				}
				name := c.Name
				if c.Self {
					name += " (You)"
				}
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						return clickable(gtx, &u.conv.header, func(gtx C) D {
							gtx.Constraints.Min.X = gtx.Constraints.Max.X
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx C) D {
									if isChannelID(c.ID) {
										return u.avatarOf(gtx, c.ID, avatarChannel, 41)
									}
									return u.avatar(gtx, c.ID, c.Name, c.IsGroup, 41)
								}),
								layout.Rigid(layout.Spacer{Width: 16}.Layout),
								layout.Flexed(1, func(gtx C) D {
									return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
										layout.Rigid(u.label(17, name, p.Text, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
										layout.Rigid(layout.Spacer{Height: 1}.Layout),
										layout.Rigid(u.label(14, sub, p.TextSecondary).Layout),
									)
								}),
							)
						})
					}),
					layout.Rigid(func(gtx C) D {
						// Video call with a drop-down arrow, like WhatsApp's call picker.
						return clickable(gtx, &u.conv.video, func(gtx C) D {
							h := gtx.Dp(40)
							if u.conv.video.Hovered() {
								fillRRect(gtx, image.Rect(0, 0, gtx.Dp(60), h), h/2, p.Hover)
							}
							gtx.Constraints = layout.Exact(image.Pt(gtx.Dp(60), h))
							return layout.Center.Layout(gtx, func(gtx C) D {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(iconW(icVideo, 27, p.IconStrong)),
									layout.Rigid(iconW(icDropDown, 22, p.IconStrong)),
								)
							})
						})
					}),
					layout.Rigid(layout.Spacer{Width: 12}.Layout),
					layout.Rigid(func(gtx C) D {
						h := gtx.Dp(24)
						fillRect(gtx, image.Rect(0, 0, max(1, gtx.Dp(1)), h), p.Divider)
						return D{Size: image.Pt(max(1, gtx.Dp(1)), h)}
					}),
					layout.Rigid(layout.Spacer{Width: 12}.Layout),
					layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &u.conv.search, icSearch, 40, 26, p.IconStrong) }),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &u.conv.menu, icMenu, 40, 26, p.IconStrong) }),
				)
			})
		})
	})
}

func dp(gtx C, px int) unit.Dp { return unit.Dp(float32(px) / gtx.Metric.PxPerDp) }

func (u *UI) layoutMessages(gtx C, c *model.Chat) D {
	rows := u.rows(c)
	width := gtx.Constraints.Max.X
	margin := max(gtx.Dp(12), min(gtx.Dp(63), width*13/100))
	maxBubble := min(width*69/100, width-2*margin)

	l := material.List(u.th, &u.conv.list)
	l.AnchorStrategy = material.Overlay
	l.Indicator.Color = u.pal.TextSecondary
	l.Indicator.Color.A = 0x50
	l.Indicator.MinorWidth = 5
	gtx.Constraints.Min = gtx.Constraints.Max
	return l.Layout(gtx, len(rows), func(gtx C, i int) D {
		r := rows[i]
		in := layout.Inset{Left: dp(gtx, margin), Right: dp(gtx, margin)}
		switch {
		case r.kind == rowDate, r.kind == rowEncryption:
			in.Top, in.Bottom = 10, 6
		case r.first:
			in.Top = 10
		default:
			in.Top = 2
		}
		if i == 0 {
			in.Top += 10
		}
		if i == len(rows)-1 {
			in.Bottom += 8
		}
		return in.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			switch {
			case r.kind == rowDate:
				return layout.N.Layout(gtx, func(gtx C) D { return u.systemChip(gtx, r.date) })
			case r.kind == rowEncryption:
				return layout.N.Layout(gtx, func(gtx C) D { return u.encryptionNotice(gtx, maxBubble) })
			default:
				return u.layoutMessageRow(gtx, c, r, maxBubble, margin)
			}
		})
	})
}

// layoutMessageRow draws one message with its interactions: the hover
// chevron and right-click menu, selection, and the flash after jumping to
// it from a reply.
func (u *UI) layoutMessageRow(gtx C, c *model.Chat, r convRow, maxW, margin int) D {
	p := u.pal
	m := r.msg
	w := gtx.Constraints.Max.X
	sel := u.conv.selecting
	rowKey := "row:" + m.ID
	if sel && u.btn(rowKey).Clicked(gtx) {
		if u.conv.picked[m.ID] {
			delete(u.conv.picked, m.ID)
		} else {
			u.conv.picked[m.ID] = true
		}
	}
	shift := 0
	if sel && !m.FromMe {
		shift = max(0, gtx.Dp(44)-margin)
	}
	cgtx := gtx
	cgtx.Constraints = layout.Constraints{Max: image.Pt(w-shift, gtx.Constraints.Max.Y)}
	bubble := record(cgtx, func(gtx C) D { return u.layoutMessage(gtx, c, r, maxW) })
	x := shift
	if m.FromMe {
		x = w - bubble.size.X
	}
	h := bubble.size.Y
	band := image.Rect(-margin, -gtx.Dp(2), w+margin, h+gtx.Dp(2))
	if u.conv.flash == m.ID {
		if u.now().Before(u.conv.flashUntil) {
			fillRect(gtx, band, argb(0x5dbf6e, 0x30))
			gtx.Execute(op.InvalidateCmd{At: u.conv.flashUntil})
		} else {
			u.conv.flash = ""
		}
	}
	if sel && u.conv.picked[m.ID] {
		fillRect(gtx, band, argb(0x5dbf6e, 0x26))
	}
	bubble.at(gtx, x, 0)
	if c.IsGroup && r.first && !m.FromMe {
		// The sender's avatar sits in the left margin, level with the bubble.
		sz := gtx.Dp(29)
		t := op.Offset(image.Pt(x-min(gtx.Dp(40), margin), 0)).Push(gtx.Ops)
		u.avatar(gtx, m.SenderID, m.Sender, false, dp(gtx, sz))
		t.Pop()
	}
	if !sel {
		t := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		hovered := u.hoverArea(gtx, m.ID, bubble.size)
		right, double := u.pressArea(gtx, m.ID, bubble.size)
		if right {
			u.openMessageMenu(m)
		}
		if double && m.Kind != model.KindDeleted {
			u.startReply(m)
		}
		chev := u.btn("chev:" + m.ID)
		if chev.Clicked(gtx) {
			u.openMessageMenu(m)
		}
		// The chevron covers part of the bubble's hover area, so it keeps
		// itself visible while it is hovered.
		if (hovered || chev.Hovered() || u.ctx.msg == m) && m.Kind != model.KindSticker {
			bg := p.BubbleIn
			if m.FromMe {
				bg = p.BubbleOut
			}
			fg := p.MetaIn
			if m.Kind == model.KindImage {
				bg, fg = argb(0x000000, 0x60), rgb(0xffffff)
			}
			ct := op.Offset(image.Pt(bubble.size.X-gtx.Dp(26+5), gtx.Dp(4))).Push(gtx.Ops)
			u.chevronButton(gtx, chev, bg, fg)
			ct.Pop()
		}
		t.Pop()
	}
	if sel {
		// The whole row toggles; a checkbox sits in the left margin.
		t := op.Offset(image.Pt(-margin, 0)).Push(gtx.Ops)
		rg := gtx
		rg.Constraints = layout.Exact(image.Pt(w+2*margin, h))
		clickable(rg, u.btn(rowKey), func(gtx C) D { return D{Size: gtx.Constraints.Max} })
		box, col := icCheckBoxEmpty, p.TextSecondary
		if u.conv.picked[m.ID] {
			box, col = icCheckBox, p.Green
		}
		bt := op.Offset(image.Pt(gtx.Dp(12), gtx.Dp(6))).Push(gtx.Ops)
		drawIcon(gtx, box, 24, col)
		bt.Pop()
		t.Pop()
	}
	return D{Size: image.Pt(w, h)}
}

func (u *UI) systemChip(gtx C, txt string) D {
	p := u.pal
	gtx.Constraints.Min = image.Point{}
	return u.card(gtx, 8, p.DateChip, func(gtx C) D {
		return layout.Inset{Left: 11, Right: 11, Top: 4, Bottom: 5}.Layout(gtx,
			u.label(13, txt, p.DateChipText, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
	})
}

func (u *UI) encryptionNotice(gtx C, maxW int) D {
	p := u.pal
	gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, max(maxW, gtx.Dp(300)))
	gtx.Constraints.Min.X = 0
	return u.card(gtx, 8, p.Encryption, func(gtx C) D {
		return layout.Inset{Left: 12, Right: 12, Top: 6, Bottom: 7}.Layout(gtx, func(gtx C) D {
			l := u.label(12.5, "🔒 Messages and calls are end-to-end encrypted. Only people in this chat can read, listen to, or share them.",
				p.EncryptionText, labelOpts{align: text.Middle})
			l.MaxLines = 0
			return l.Layout(gtx)
		})
	})
}

// card draws w on a rounded rectangle. Light mode adds WhatsApp's 1px shadow.
func (u *UI) card(gtx C, radius unit.Dp, bg color.NRGBA, w layout.Widget) D {
	m := op.Record(gtx.Ops)
	dims := w(gtx)
	call := m.Stop()
	r := gtx.Dp(radius)
	rect := image.Rectangle{Max: dims.Size}
	if !u.dark {
		fillRRect(gtx, rect.Add(image.Pt(0, 1)), r, argb(0x0b141a, 0x21))
	}
	fillRRect(gtx, rect, r, bg)
	call.Add(gtx.Ops)
	return dims
}

// nbspWidth returns the advance of a non-breaking space at the given size.
// Bubbles pad their text with NBSPs so the timestamp can sit on the last line
// when it fits (the same trick WhatsApp Web uses with an inline spacer).
func (u *UI) nbspWidth(gtx C, size unit.Sp) float32 {
	px := gtx.Sp(size)
	if w, ok := u.conv.nbsp[px]; ok {
		return w
	}
	const n = 20
	measure := func(s string) int {
		m := op.Record(gtx.Ops)
		gtx := gtx
		gtx.Constraints = layout.Constraints{Max: image.Pt(1<<20, 1<<20)}
		d := u.label(size, s, color.NRGBA{}).Layout(gtx)
		m.Stop()
		return d.Size.X
	}
	spaces := make([]rune, n)
	for i := range spaces {
		spaces[i] = '\u00a0'
	}
	w := float32(measure("x"+string(spaces)+"x")-measure("xx")) / n
	if w <= 0 {
		w = float32(gtx.Sp(size)) * 0.27
	}
	if u.conv.nbsp == nil {
		u.conv.nbsp = make(map[int]float32)
	}
	u.conv.nbsp[px] = w
	return w
}

func (u *UI) layoutMeta(gtx C, m *model.Message, col color.NRGBA, tickCol *color.NRGBA) D {
	gtx.Constraints.Min = image.Point{}
	var children []layout.FlexChild
	if m.Starred {
		children = append(children,
			layout.Rigid(iconW(icStarFill, 14, col)),
			layout.Rigid(layout.Spacer{Width: 3}.Layout))
	}
	children = append(children, layout.Rigid(u.label(12.5, m.Time.Format("15:04"), col).Layout))
	if m.FromMe && m.Kind != model.KindDeleted {
		ic, tc := receiptIcon(m.Receipt, u.pal, true)
		if m.Receipt != model.Read {
			tc = col
		}
		if tickCol != nil && m.Receipt != model.Read {
			tc = *tickCol
		}
		children = append(children,
			layout.Rigid(layout.Spacer{Width: 3}.Layout),
			layout.Rigid(iconW(ic, 17, tc)),
		)
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
}

type part struct {
	call op.CallOp
	size image.Point
}

func record(gtx C, w layout.Widget) part {
	m := op.Record(gtx.Ops)
	d := w(gtx)
	return part{m.Stop(), d.Size}
}

func (p part) at(gtx C, x, y int) {
	t := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
	p.call.Add(gtx.Ops)
	t.Pop()
}

// layoutMessage draws a bubble (or a sticker) plus its reaction pill.
func (u *UI) layoutMessage(gtx C, c *model.Chat, r convRow, maxW int) D {
	m := r.msg
	var dims D
	if m.Kind == model.KindSticker {
		dims = u.layoutSticker(gtx, m)
	} else {
		dims = u.layoutBubble(gtx, c, m, r.first, maxW)
	}
	if m.Reaction == "" {
		return dims
	}
	p := u.pal
	bg := p.BubbleIn
	pill := record(gtx, func(gtx C) D {
		gtx.Constraints.Min = image.Point{}
		return u.card(gtx, 13, bg, func(gtx C) D {
			return layout.Inset{Left: 6, Right: 6, Top: 2, Bottom: 3}.Layout(gtx, u.label(14, m.Reaction, p.Text).Layout)
		})
	})
	x := gtx.Dp(8)
	if m.FromMe {
		x = dims.Size.X - pill.size.X - gtx.Dp(8)
	}
	ring := gtx.Dp(2)
	py := dims.Size.Y - gtx.Dp(5)
	fillRRect(gtx, image.Rect(x-ring, py-ring, x+pill.size.X+ring, py+pill.size.Y+ring), pill.size.Y/2+ring, p.ChatBg)
	pill.at(gtx, x, py)
	dims.Size.Y = py + pill.size.Y + ring
	return dims
}

// layoutBubble draws one message bubble, sized to its content.
func (u *UI) layoutBubble(gtx C, c *model.Chat, m *model.Message, tail bool, maxW int) D {
	p := u.pal
	out := m.FromMe
	bg, quoteBg, textCol, metaCol, secondary := p.BubbleIn, p.QuoteIn, p.Text, p.MetaIn, p.TextSecondary
	if out {
		bg, quoteBg, textCol, metaCol, secondary = p.BubbleOut, p.QuoteOut, p.TextOut, p.MetaOut, p.SecondaryOut
	}
	isImg := m.Kind == model.KindImage

	padL, padR, padT, padB := gtx.Dp(9), gtx.Dp(8), gtx.Dp(6), gtx.Dp(8)
	if isImg {
		padL, padR, padT, padB = gtx.Dp(3), gtx.Dp(3), gtx.Dp(3), gtx.Dp(3)
	}
	inner := maxW - padL - padR
	cgtx := gtx
	cgtx.Constraints = layout.Constraints{Max: image.Pt(inner, 1<<20)}

	// Timestamp and receipt ticks.
	metaOnImage := isImg && m.Text == ""
	var tickCol *color.NRGBA
	if metaOnImage {
		white := rgb(0xffffff)
		metaCol, tickCol = white, &white
	}
	meta := record(cgtx, func(gtx C) D { return u.layoutMeta(gtx, m, metaCol, tickCol) })

	// Measure natural widths first; the quote and image stretch to the widest part.
	contentW := 0
	var sender, body part
	hasSender := c.IsGroup && !out && tail && m.Sender != ""
	if hasSender {
		col := p.Senders[hashIndex(m.SenderID+m.Sender, len(p.Senders))]
		sender = record(cgtx, u.label(13, m.Sender, col, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout)
		contentW = max(contentW, sender.size.X)
	}

	imgW, imgH := 0, 0
	textInset := 0 // horizontal inset of text inside image bubbles
	var img *imgEntry
	if isImg {
		imgW = min(inner, gtx.Dp(330))
		imgH = imgW * 3 / 4
		img = u.messageImage(m, gtx.Dp(330))
		if img != nil && img.state == imgReady {
			ratio := float32(img.size.Y) / float32(img.size.X)
			imgH = int(float32(imgW) * min(max(ratio, 0.4), 1.4))
		}
		contentW = max(contentW, imgW)
		textInset = gtx.Dp(6)
	}

	const textSize = unit.Sp(15.7)
	text := m.Text
	italic := false
	var lead layout.Widget // icon before the text (media types, deleted)
	switch {
	case m.Kind == model.KindDeleted:
		text, textCol, italic = "This message was deleted", secondary, true
		if out {
			text = "You deleted this message"
		}
		lead = iconW(icBlock, 19, secondary)
	case !isImg && m.Media != model.MediaNone:
		lead = iconW(mediaIcon(m.Media), 20, secondary)
		text = mediaLabel(m)
		if m.Media == model.MediaVoice {
			text = "Voice message · " + mediaLabel(m)
		}
	}
	leadW := 0
	if lead != nil {
		leadW = gtx.Dp(25)
	}
	if text != "" {
		nbsp := u.nbspWidth(gtx, textSize)
		spacer := make([]rune, int(float32(meta.size.X+gtx.Dp(8))/nbsp)+1)
		for i := range spacer {
			spacer[i] = '\u00a0'
		}
		prefix := ""
		if leadW > 0 {
			indent := make([]rune, int(float32(leadW)/nbsp)+1)
			for i := range indent {
				indent[i] = '\u00a0'
			}
			prefix = string(indent)
		}
		tgtx := cgtx
		if isImg {
			tgtx.Constraints.Max.X = imgW - 2*textInset
		}
		spans := u.richSpans(text, textSize, textCol, italic)
		plain := font.Font{Typeface: typeface}
		if prefix != "" {
			spans = append([]styledtext.SpanStyle{{Font: plain, Size: textSize, Color: textCol, Content: prefix}}, spans...)
		}
		spans = append(spans, styledtext.SpanStyle{Font: plain, Size: textSize, Color: textCol, Content: " " + string(spacer)})
		st := styledtext.Text(u.th.Shaper, spans...)
		st.LineHeight, st.LineHeightScale = 22, 1
		body = record(tgtx, func(gtx C) D { return st.Layout(gtx, nil) })
		contentW = max(contentW, body.size.X+2*textInset)
	} else if !isImg {
		contentW = max(contentW, meta.size.X)
	}

	var fwd part
	if m.Forwarded && m.Kind != model.KindDeleted {
		fwd = record(cgtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(iconW(icForward, 16, secondary)),
				layout.Rigid(layout.Spacer{Width: 4}.Layout),
				layout.Rigid(u.label(13, "Forwarded", secondary, labelOpts{italic: true, maxLines: 1}).Layout),
			)
		})
		contentW = max(contentW, fwd.size.X+2*textInset)
	}

	var quote part
	if m.Quote != nil {
		var qm *model.Message
		for _, x := range u.msgs {
			if x.ID == m.Quote.ID {
				qm = x
			}
		}
		qw := min(inner, max(contentW, gtx.Dp(180)))
		quote = record(cgtx, func(gtx C) D { return u.layoutQuote(gtx, m.Quote, quoteBg, secondary, qw, qm) })
		contentW = max(contentW, quote.size.X)
		if quote.size.X < contentW {
			quote = record(cgtx, func(gtx C) D { return u.layoutQuote(gtx, m.Quote, quoteBg, secondary, contentW, qm) })
		}
	}
	if u.btn("quote:"+m.ID).Clicked(gtx) && m.Quote != nil && m.Quote.ID != "" {
		u.jumpTo(m.Quote.ID)
	}
	if u.btn("img:" + m.ID).Clicked(gtx) {
		u.openViewer(m)
	}

	// Place everything, then paint the bubble behind it.
	macro := op.Record(gtx.Ops)
	y := 0
	if hasSender {
		sx := 0
		if isImg {
			sx = textInset
			y += gtx.Dp(3)
		}
		sender.at(gtx, sx, y)
		y += sender.size.Y + gtx.Dp(2)
		if isImg {
			y += gtx.Dp(3)
		}
	}
	if m.Forwarded && m.Kind != model.KindDeleted {
		fx := 0
		if isImg {
			fx = textInset
			y += gtx.Dp(2)
		}
		fwd.at(gtx, fx, y)
		y += fwd.size.Y + gtx.Dp(3)
	}
	if m.Quote != nil {
		quote.at(gtx, 0, y)
		func() {
			t := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
			defer t.Pop()
			qg := gtx
			qg.Constraints = layout.Exact(quote.size)
			clickable(qg, u.btn("quote:"+m.ID), func(gtx C) D { return D{Size: quote.size} })
		}()
		y += quote.size.Y + gtx.Dp(5)
	}
	if isImg {
		u.layoutImage(gtx, image.Rect(0, y, imgW, y+imgH), m, img)
		func() {
			t := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
			defer t.Pop()
			ig := gtx
			ig.Constraints = layout.Exact(image.Pt(imgW, imgH))
			clickable(ig, u.btn("img:"+m.ID), func(gtx C) D { return D{Size: gtx.Constraints.Max} })
		}()
		y += imgH
		if metaOnImage {
			meta.at(gtx, imgW-meta.size.X-gtx.Dp(7), y-meta.size.Y-gtx.Dp(5))
		} else {
			y += gtx.Dp(5)
		}
	}
	if text != "" {
		body.at(gtx, textInset, y)
		if lead != nil {
			t := op.Offset(image.Pt(textInset, y)).Push(gtx.Ops)
			lead(gtx)
			t.Pop()
		}
		y += body.size.Y
		meta.at(gtx, contentW-meta.size.X-textInset, y-meta.size.Y+gtx.Dp(4))
		if isImg {
			y += gtx.Dp(5)
		}
	} else if !isImg {
		meta.at(gtx, contentW-meta.size.X, y)
		y += meta.size.Y
	}
	content := macro.Stop()

	w := contentW + padL + padR
	h := y + padT + padB
	u.paintBubble(gtx, w, h, bg, out, tail)
	t := op.Offset(image.Pt(padL, padT)).Push(gtx.Ops)
	content.Add(gtx.Ops)
	t.Pop()
	return D{Size: image.Pt(w, h)}
}

// paintBubble paints the bubble body and its tail (plus a 1px shadow in light mode).
func (u *UI) paintBubble(gtx C, w, h int, bg color.NRGBA, out, tail bool) {
	r := gtx.Dp(8)
	rr := clip.RRect{Rect: image.Rect(0, 0, w, h), NW: r, NE: r, SW: r, SE: r}
	if tail {
		if out {
			rr.NE = 0
		} else {
			rr.NW = 0
		}
	}
	if !u.dark {
		shadow := rr
		shadow.Rect = shadow.Rect.Add(image.Pt(0, 1))
		paint.FillShape(gtx.Ops, argb(0x0b141a, 0x21), shadow.Op(gtx.Ops))
	}
	paint.FillShape(gtx.Ops, bg, rr.Op(gtx.Ops))
	if !tail {
		return
	}
	// The tail is a small curved triangle hanging off the top corner.
	tw, th := float32(gtx.Dp(8)), float32(gtx.Dp(13))
	var path clip.Path
	path.Begin(gtx.Ops)
	if out {
		x := float32(w)
		path.MoveTo(f32.Pt(x-1, 0))
		path.LineTo(f32.Pt(x+tw-2, 0))
		path.QuadTo(f32.Pt(x+tw, 0), f32.Pt(x+tw-1.2, 1.6))
		path.LineTo(f32.Pt(x, th))
		path.LineTo(f32.Pt(x-1, th))
	} else {
		path.MoveTo(f32.Pt(1, 0))
		path.LineTo(f32.Pt(-tw+2, 0))
		path.QuadTo(f32.Pt(-tw, 0), f32.Pt(-tw+1.2, 1.6))
		path.LineTo(f32.Pt(0, th))
		path.LineTo(f32.Pt(1, th))
	}
	path.Close()
	paint.FillShape(gtx.Ops, bg, clip.Outline{Path: path.End()}.Op())
}

// layoutQuote draws a quoted message: a colored bar, the author and a
// snippet, plus the quoted picture's thumbnail when it's loaded (qm).
func (u *UI) layoutQuote(gtx C, q *model.Quote, bg, secondary color.NRGBA, width int, qm *model.Message) D {
	p := u.pal
	name := q.Sender
	if name == "" {
		name = "You"
	}
	name = plainText(name)
	col := p.Senders[hashIndex(name, len(p.Senders))]
	if name == "You" {
		col = p.Green
	}
	txt := q.Text
	if q.Media != model.MediaNone {
		txt = mediaLabel(&model.Message{Media: q.Media, Text: q.Text})
	}
	gtx.Constraints.Min.X = width
	gtx.Constraints.Max.X = width
	m := op.Record(gtx.Ops)
	right := unit.Dp(10)
	if qm != nil && qm.Kind == model.KindImage {
		right = 66 // room for the thumbnail
	}
	dims := layout.Inset{Left: 12, Right: right, Top: 6, Bottom: 7}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(u.label(13, name, col, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
			layout.Rigid(layout.Spacer{Height: 1}.Layout),
			layout.Rigid(func(gtx C) D {
				children := []layout.FlexChild{}
				if ic := mediaIcon(q.Media); ic != nil {
					children = append(children, layout.Rigid(func(gtx C) D {
						return layout.Inset{Right: 4}.Layout(gtx, iconW(ic, 16, secondary))
					}))
				}
				children = append(children, layout.Flexed(1, u.label(13.5, plainText(txt), secondary, labelOpts{maxLines: 2}).Layout))
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
			}),
		)
	})
	call := m.Stop()
	dims.Size.X = width
	r := gtx.Dp(7)
	defer clip.UniformRRect(image.Rectangle{Max: dims.Size}, r).Push(gtx.Ops).Pop()
	fillRect(gtx, image.Rectangle{Max: dims.Size}, bg)
	fillRect(gtx, image.Rect(0, 0, gtx.Dp(4), dims.Size.Y), col)
	call.Add(gtx.Ops)
	if qm != nil && qm.Kind == model.KindImage {
		s := dims.Size.Y
		tr := image.Rect(width-s, 0, width, s)
		if img := u.messageImage(qm, s*2); img != nil && img.state == imgReady {
			paintCover(gtx, img.op, img.size, tr)
		} else if qm.ImageA != 0 || qm.ImageB != 0 {
			u.gradientImage(gtx, tr, qm.ImageA, qm.ImageB)
		}
	}
	return dims
}

// messageImage returns the best available picture for an image message:
// the downloaded media, or the embedded thumbnail while that loads.
func (u *UI) messageImage(m *model.Message, maxPx int) *imgEntry {
	b := u.backend
	if m.Media == model.MediaImage || m.Media == model.MediaSticker {
		full := u.images.get("m:"+m.ChatID+"/"+m.ID, maxPx, func() []byte { return b.MediaData(m.ChatID, m.ID) })
		if full.state == imgReady {
			return full
		}
	}
	if len(m.Thumb) > 0 {
		thumb := m.Thumb
		return u.images.get("t:"+m.ChatID+"/"+m.ID, maxPx, func() []byte { return thumb })
	}
	return nil
}

// layoutImage draws a picture preview in r: the image, the demo gradient,
// or a neutral placeholder. Videos get a play button and duration.
func (u *UI) layoutImage(gtx C, r image.Rectangle, m *model.Message, img *imgEntry) {
	func() {
		defer clip.UniformRRect(r, gtx.Dp(6)).Push(gtx.Ops).Pop()
		switch {
		case img != nil && img.state == imgReady:
			paintCover(gtx, img.op, img.size, r)
		case m.ImageA != 0 || m.ImageB != 0:
			u.gradientImage(gtx, r, m.ImageA, m.ImageB)
		default:
			fillRect(gtx, r, u.pal.Hover)
			isz := gtx.Dp(48)
			t := op.Offset(r.Min.Add(r.Size().Div(2)).Sub(image.Pt(isz/2, isz/2))).Push(gtx.Ops)
			drawIcon(gtx, icImage, 48, u.pal.TextSecondary)
			t.Pop()
		}
	}()
	if m.Media != model.MediaVideo && m.Media != model.MediaGIF {
		return
	}
	c := r.Min.Add(r.Size().Div(2))
	rad := gtx.Dp(26)
	fillCircle(gtx, c, rad, argb(0x000000, 0x80))
	var tri clip.Path
	tri.Begin(gtx.Ops)
	s := float32(rad) * 0.45
	cf := f32.Pt(float32(c.X), float32(c.Y))
	tri.MoveTo(cf.Add(f32.Pt(-s*0.6, -s)))
	tri.LineTo(cf.Add(f32.Pt(s, 0)))
	tri.LineTo(cf.Add(f32.Pt(-s*0.6, s)))
	tri.Close()
	paint.FillShape(gtx.Ops, rgb(0xffffff), clip.Outline{Path: tri.End()}.Op())
	if m.Duration > 0 {
		d := record(gtx, u.label(11.5, fmt.Sprintf("%d:%02d", m.Duration/60, m.Duration%60), rgb(0xffffff)).Layout)
		d.at(gtx, r.Min.X+gtx.Dp(8), r.Max.Y-d.size.Y-gtx.Dp(6))
	}
}

// layoutSticker draws a sticker without a bubble, with the time on a chip.
func (u *UI) layoutSticker(gtx C, m *model.Message) D {
	p := u.pal
	sz := gtx.Dp(150)
	img := u.messageImage(m, sz*2)
	r := image.Rect(0, 0, sz, sz)
	if img != nil && img.state == imgReady {
		// Stickers keep their aspect ratio inside the square.
		s := min(float32(sz)/float32(img.size.X), float32(sz)/float32(img.size.Y))
		w, h := int(float32(img.size.X)*s), int(float32(img.size.Y)*s)
		dst := image.Rect((sz-w)/2, (sz-h)/2, (sz-w)/2+w, (sz-h)/2+h)
		paintCover(gtx, img.op, img.size, dst)
	} else {
		fillRRect(gtx, r, gtx.Dp(12), argb(0x808080, 0x30))
	}
	meta := record(gtx, func(gtx C) D {
		return u.card(gtx, 8, p.BubbleIn, func(gtx C) D {
			return layout.Inset{Left: 6, Right: 6, Top: 2, Bottom: 3}.Layout(gtx, func(gtx C) D {
				return u.layoutMeta(gtx, m, p.MetaIn, nil)
			})
		})
	})
	meta.at(gtx, sz-meta.size.X, sz-meta.size.Y)
	return D{Size: image.Pt(sz, sz)}
}

// gradientImage stands in for photos in demo data.
func (u *UI) gradientImage(gtx C, r image.Rectangle, a, b uint32) {
	paint.LinearGradientOp{
		Stop1: f32.Pt(float32(r.Min.X), float32(r.Min.Y)), Color1: rgb(a),
		Stop2: f32.Pt(float32(r.Max.X), float32(r.Max.Y)), Color2: rgb(b),
	}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	w, h := r.Dx(), r.Dy()
	fillCircle(gtx, image.Pt(r.Min.X+w*3/4, r.Min.Y+h/4), h/8, argb(0xffffff, 0xb0))
	var path clip.Path
	path.Begin(gtx.Ops)
	path.MoveTo(f32.Pt(float32(r.Min.X), float32(r.Max.Y)))
	path.LineTo(f32.Pt(float32(r.Min.X), float32(r.Min.Y+h*3/4)))
	path.QuadTo(f32.Pt(float32(r.Min.X+w/4), float32(r.Min.Y+h/2)), f32.Pt(float32(r.Min.X+w/2), float32(r.Min.Y+h*3/4)))
	path.QuadTo(f32.Pt(float32(r.Min.X+w*3/4), float32(r.Min.Y+h)), f32.Pt(float32(r.Max.X), float32(r.Min.Y+h*5/8)))
	path.LineTo(f32.Pt(float32(r.Max.X), float32(r.Max.Y)))
	path.Close()
	paint.FillShape(gtx.Ops, argb(0x0b3d2e, 0x90), clip.Outline{Path: path.End()}.Op())
	paint.LinearGradientOp{
		Stop1: f32.Pt(0, float32(r.Max.Y-gtx.Dp(40))), Color1: color.NRGBA{},
		Stop2: f32.Pt(0, float32(r.Max.Y)), Color2: color.NRGBA{A: 0x70},
	}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// layoutEmpty is the welcome pane shown when no chat is open.
func (u *UI) layoutEmpty(gtx C) D {
	p := u.pal
	dims := fill(gtx, p.Panel)
	gtx.Constraints.Min = gtx.Constraints.Max
	layout.Center.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(460))
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				sz := gtx.Dp(150)
				fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, p.Hover)
				return centerIn(gtx, sz, iconW(icChats, 72, p.Green))
			}),
			layout.Rigid(layout.Spacer{Height: 28}.Layout),
			layout.Rigid(u.label(30, "WazzapClients for Windows", p.Text, labelOpts{weight: font.Light, maxLines: 1, align: text.Middle}).Layout),
			layout.Rigid(layout.Spacer{Height: 14}.Layout),
			layout.Rigid(u.label(14, "Send and receive messages without keeping your phone online. Native, lightweight, and no browser inside.",
				p.TextSecondary, labelOpts{maxLines: 0, align: text.Middle}).Layout),
		)
	})
	layout.S.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = 0 // S only clears Min.Y
		return layout.Inset{Bottom: 36}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(iconW(icLock, 14, p.TextSecondary)),
				layout.Rigid(layout.Spacer{Width: 5}.Layout),
				layout.Rigid(u.label(13, "Your personal messages are end-to-end encrypted", p.TextSecondary).Layout),
			)
		})
	})
	return dims
}
