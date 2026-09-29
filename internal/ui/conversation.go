package ui

import (
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
)

type rowKind int

const (
	rowDate rowKind = iota
	rowEncryption
	rowMessage
)

// convRow is one entry of the message list: a day separator or a message.
type convRow struct {
	kind rowKind
	date string
	msg  *model.Message
	// first marks the first message of a run from the same sender. It gets
	// the bubble tail and extra space above.
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
		first := newDay || prev.FromMe != m.FromMe || prev.Sender != m.Sender ||
			m.Time.Sub(prev.Time) > 10*time.Minute
		rows = append(rows, convRow{kind: rowMessage, msg: m, first: first})
		prev = m
	}
	u.conv.rows, u.conv.rowsFor, u.conv.rowsVer = rows, c, u.msgsVer
	return rows
}

func (u *UI) layoutConversation(gtx C) D {
	c := u.selected
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return u.layoutConvHeader(gtx, c) }),
		layout.Flexed(1, func(gtx C) D {
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx C) D {
					return u.conv.wallpaper.layout(gtx, u.pal.ChatBg, u.pal.Doodle)
				}),
				layout.Stacked(func(gtx C) D { return u.layoutMessages(gtx, c) }),
			)
		}),
		layout.Rigid(u.layoutComposer),
	)
}

func (u *UI) layoutConvHeader(gtx C, c *model.Chat) D {
	p := u.pal
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return background(gtx, p.Header, 0, func(gtx C) D {
		gtx.Constraints.Min.Y = gtx.Dp(60)
		return layout.Inset{Left: 16, Right: 12}.Layout(gtx, func(gtx C) D {
			sub := c.Presence
			if c.Typing != "" {
				sub = "typing…"
				if c.IsGroup {
					sub = c.Typing + " is typing…"
				}
			}
			if sub == "" {
				sub = "click here for contact info"
			}
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					return clickable(gtx, &u.conv.header, func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D { return u.avatar(gtx, c.Name, c.IsGroup, 40) }),
							layout.Rigid(layout.Spacer{Width: 15}.Layout),
							layout.Flexed(1, func(gtx C) D {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(u.label(16, c.Name, p.Text, labelOpts{weight: font.Medium, maxLines: 1}).Layout),
									layout.Rigid(u.label(13, sub, p.TextSecondary).Layout),
								)
							}),
						)
					})
				}),
				layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &u.conv.video, icVideo, p.Icon, false) }),
				layout.Rigid(layout.Spacer{Width: 6}.Layout),
				layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &u.conv.call, icCall, p.Icon, false) }),
				layout.Rigid(layout.Spacer{Width: 6}.Layout),
				layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &u.conv.search, icSearch, p.Icon, false) }),
				layout.Rigid(layout.Spacer{Width: 6}.Layout),
				layout.Rigid(func(gtx C) D { return u.iconButton(gtx, &u.conv.menu, icMenu, p.Icon, false) }),
			)
		})
	})
}

func (u *UI) layoutMessages(gtx C, c *model.Chat) D {
	rows := u.rows(c)
	width := gtx.Constraints.Max.X
	margin := max(gtx.Dp(16), min(width*6/100, gtx.Dp(64)))
	maxBubble := (width - 2*margin) * 65 / 100
	if width-2*margin < gtx.Dp(500) {
		maxBubble = (width - 2*margin) * 85 / 100
	}

	l := material.List(u.th, &u.conv.list)
	l.AnchorStrategy = material.Overlay
	l.Indicator.Color = color.NRGBA{A: 0x40}
	gtx.Constraints.Min = gtx.Constraints.Max
	return l.Layout(gtx, len(rows), func(gtx C, i int) D {
		r := rows[i]
		in := layout.Inset{Left: unit.Dp(float32(margin) / gtx.Metric.PxPerDp), Right: unit.Dp(float32(margin) / gtx.Metric.PxPerDp)}
		switch {
		case r.kind == rowDate, r.kind == rowEncryption:
			in.Top, in.Bottom = 10, 6
		case r.first:
			in.Top = 8
		default:
			in.Top = 2
		}
		if i == 0 {
			in.Top += 12
		}
		if i == len(rows)-1 {
			in.Bottom += 12
		}
		return in.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			switch {
			case r.kind == rowDate:
				return layout.N.Layout(gtx, func(gtx C) D { return u.systemChip(gtx, r.date) })
			case r.kind == rowEncryption:
				return layout.N.Layout(gtx, func(gtx C) D { return u.encryptionNotice(gtx, maxBubble) })
			case r.msg.FromMe:
				return layout.NE.Layout(gtx, func(gtx C) D { return u.layoutBubble(gtx, c, r.msg, r.first, maxBubble) })
			default:
				return layout.NW.Layout(gtx, func(gtx C) D { return u.layoutBubble(gtx, c, r.msg, r.first, maxBubble) })
			}
		})
	})
}

func (u *UI) systemChip(gtx C, txt string) D {
	p := u.pal
	gtx.Constraints.Min = image.Point{}
	return u.shadowed(gtx, 8, p.SystemChip, func(gtx C) D {
		return layout.Inset{Left: 12, Right: 12, Top: 5, Bottom: 6}.Layout(gtx,
			u.label(12.5, txt, p.SystemChipText).Layout)
	})
}

func (u *UI) encryptionNotice(gtx C, maxW int) D {
	p := u.pal
	gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, max(maxW, gtx.Dp(300)))
	gtx.Constraints.Min.X = 0
	return u.shadowed(gtx, 8, p.Encryption, func(gtx C) D {
		return layout.Inset{Left: 12, Right: 12, Top: 6, Bottom: 7}.Layout(gtx, func(gtx C) D {
			l := u.label(12.5, "🔒 Messages and calls are end-to-end encrypted. Only people in this chat can read, listen to, or share them. Click to learn more.",
				p.EncryptionText, labelOpts{align: text.Middle})
			l.MaxLines = 0
			return l.Layout(gtx)
		})
	})
}

// shadowed draws w on a rounded card with WhatsApp's 1px bottom shadow.
func (u *UI) shadowed(gtx C, radius unit.Dp, bg color.NRGBA, w layout.Widget) D {
	m := op.Record(gtx.Ops)
	dims := w(gtx)
	call := m.Stop()
	r := gtx.Dp(radius)
	rect := image.Rectangle{Max: dims.Size}
	fillRRect(gtx, rect.Add(image.Pt(0, 1)), r, u.pal.Shadow)
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
	w := u.measureNBSP(gtx, size)
	if u.conv.nbsp == nil {
		u.conv.nbsp = make(map[int]float32)
	}
	u.conv.nbsp[px] = w
	return w
}

func (u *UI) measureNBSP(gtx C, size unit.Sp) float32 {
	const n = 20
	measure := func(s string) int {
		m := op.Record(gtx.Ops)
		gtx.Constraints = layout.Constraints{Max: image.Pt(1<<20, 1<<20)}
		d := u.label(size, s, color.NRGBA{}).Layout(gtx)
		m.Stop()
		return d.Size.X
	}
	spaces := make([]rune, n)
	for i := range spaces {
		spaces[i] = ' '
	}
	w := measure("x"+string(spaces)+"x") - measure("xx")
	if w <= 0 {
		return float32(gtx.Sp(size)) * 0.27
	}
	return float32(w) / n
}

func (u *UI) layoutMeta(gtx C, m *model.Message, col color.NRGBA, tickCol *color.NRGBA) D {
	gtx.Constraints.Min = image.Point{}
	children := []layout.FlexChild{
		layout.Rigid(u.label(11, m.Time.Format("15:04"), col).Layout),
	}
	if m.FromMe {
		data, tc := receiptIcon(m.Receipt, u.pal)
		if tickCol != nil && m.Receipt != model.Read {
			tc = *tickCol
		}
		children = append(children,
			layout.Rigid(layout.Spacer{Width: 3}.Layout),
			layout.Rigid(func(gtx C) D { return u.icons.layout(gtx, data, 16, tc) }),
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

// layoutBubble draws one message bubble, sized to its content.
func (u *UI) layoutBubble(gtx C, c *model.Chat, m *model.Message, tail bool, maxW int) D {
	p := u.pal
	out := m.FromMe
	bg := p.BubbleIn
	quoteBg := p.QuoteIn
	if out {
		bg, quoteBg = p.BubbleOut, p.QuoteOut
	}
	isImg := m.Kind == model.KindImage

	padL, padR, padT, padB := gtx.Dp(9), gtx.Dp(7), gtx.Dp(6), gtx.Dp(8)
	if isImg {
		padL, padR, padT, padB = gtx.Dp(3), gtx.Dp(3), gtx.Dp(3), gtx.Dp(3)
	}
	inner := maxW - padL - padR
	cgtx := gtx
	cgtx.Constraints = layout.Constraints{Max: image.Pt(inner, 1<<20)}

	// Timestamp and receipt ticks.
	metaOnImage := isImg && m.Text == ""
	metaCol := p.Meta
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
		col := rgb(senderColors[hashIndex(m.Sender, len(senderColors))])
		sender = record(cgtx, u.label(12.8, m.Sender, col, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout)
		contentW = max(contentW, sender.size.X)
	}

	imgW, imgH := 0, 0
	textInset := 0 // horizontal inset of text inside image bubbles
	if isImg {
		imgW = min(inner, gtx.Dp(330))
		imgH = imgW * 3 / 4
		if t := u.thumb(m); t.ok {
			ratio := float32(t.size.Y) / float32(t.size.X)
			imgH = int(float32(imgW) * min(max(ratio, 0.5), 1.3))
		}
		contentW = max(contentW, imgW)
		textInset = gtx.Dp(6)
	}

	const textSize = unit.Sp(14.5)
	text, textCol := m.Text, p.Text
	if m.Kind == model.KindDeleted {
		text, textCol = "🚫 This message was deleted", p.TextSecondary
	}
	if text != "" {
		n := int(float32(meta.size.X+gtx.Dp(8))/u.nbspWidth(gtx, textSize)) + 1
		spacer := make([]rune, n)
		for i := range spacer {
			spacer[i] = ' '
		}
		tgtx := cgtx
		if isImg {
			tgtx.Constraints.Max.X = imgW - 2*textInset
		}
		l := u.label(textSize, text+" "+string(spacer), textCol)
		l.MaxLines = 0
		l.LineHeight = 19
		body = record(tgtx, l.Layout)
		contentW = max(contentW, body.size.X+2*textInset)
	} else if !isImg {
		contentW = max(contentW, meta.size.X)
	}

	var quote part
	if m.Quote != nil {
		qw := min(inner, max(contentW, gtx.Dp(180)))
		quote = record(cgtx, func(gtx C) D { return u.layoutQuote(gtx, m.Quote, quoteBg, qw) })
		contentW = max(contentW, quote.size.X)
		if quote.size.X < contentW {
			quote = record(cgtx, func(gtx C) D { return u.layoutQuote(gtx, m.Quote, quoteBg, contentW) })
		}
	}

	// Place everything, then paint the bubble behind it.
	macro := op.Record(gtx.Ops)
	y := 0
	if hasSender {
		sender.at(gtx, 0, y)
		y += sender.size.Y + gtx.Dp(2)
		if isImg {
			y += gtx.Dp(3)
		}
	}
	if m.Quote != nil {
		quote.at(gtx, 0, y)
		y += quote.size.Y + gtx.Dp(4)
	}
	if isImg {
		u.layoutImage(gtx, image.Rect(0, y, imgW, y+imgH), m)
		y += imgH
		if metaOnImage {
			meta.at(gtx, imgW-meta.size.X-gtx.Dp(7), y-meta.size.Y-gtx.Dp(5))
		} else {
			y += gtx.Dp(5)
		}
	}
	if text != "" {
		body.at(gtx, textInset, y)
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

	dims := D{Size: image.Pt(w, h)}
	if m.Reaction != "" {
		pill := record(gtx, func(gtx C) D {
			gtx.Constraints.Min = image.Point{}
			return u.shadowed(gtx, 12, p.BubbleIn, func(gtx C) D {
				return layout.Inset{Left: 6, Right: 6, Top: 2, Bottom: 3}.Layout(gtx, u.label(13, m.Reaction, p.Text).Layout)
			})
		})
		x := gtx.Dp(8)
		if out {
			x = w - pill.size.X - gtx.Dp(8)
		}
		ring := gtx.Dp(2)
		py := h - gtx.Dp(4)
		fillRRect(gtx, image.Rect(x-ring, py-ring, x+pill.size.X+ring, py+pill.size.Y+ring), pill.size.Y/2+ring, p.ChatBg)
		pill.at(gtx, x, py)
		dims.Size.Y = py + pill.size.Y + ring
	}
	return dims
}

// paintBubble paints the bubble body, its tail and the 1px drop shadow.
func (u *UI) paintBubble(gtx C, w, h int, bg color.NRGBA, out, tail bool) {
	r := gtx.Dp(7.5)
	rr := clip.RRect{Rect: image.Rect(0, 0, w, h), NW: r, NE: r, SW: r, SE: r}
	if tail {
		if out {
			rr.NE = 0
		} else {
			rr.NW = 0
		}
	}
	shadow := rr
	shadow.Rect = shadow.Rect.Add(image.Pt(0, 1))
	paint.FillShape(gtx.Ops, u.pal.Shadow, shadow.Op(gtx.Ops))
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

func (u *UI) layoutQuote(gtx C, q *model.Quote, bg color.NRGBA, width int) D {
	col := rgb(senderColors[hashIndex(q.Sender, len(senderColors))])
	name := q.Sender
	if name == "" {
		name = "You"
	}
	gtx.Constraints.Min.X = width
	gtx.Constraints.Max.X = width
	m := op.Record(gtx.Ops)
	dims := layout.Inset{Left: 12, Right: 10, Top: 6, Bottom: 7}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(u.label(12.8, name, col, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
			layout.Rigid(layout.Spacer{Height: 1}.Layout),
			layout.Rigid(u.label(13.2, q.Text, u.pal.TextSecondary, labelOpts{maxLines: 2}).Layout),
		)
	})
	call := m.Stop()
	dims.Size.X = width
	r := gtx.Dp(7.5)
	defer clip.UniformRRect(image.Rectangle{Max: dims.Size}, r).Push(gtx.Ops).Pop()
	fillRect(gtx, image.Rectangle{Max: dims.Size}, bg)
	fillRect(gtx, image.Rect(0, 0, gtx.Dp(4), dims.Size.Y), col)
	call.Add(gtx.Ops)
	return dims
}

// gradientImage stands in for photo thumbnails until media download exists.
func (u *UI) gradientImage(gtx C, r image.Rectangle, a, b uint32) {
	defer clip.UniformRRect(r, gtx.Dp(6)).Push(gtx.Ops).Pop()
	paint.LinearGradientOp{
		Stop1: f32.Pt(float32(r.Min.X), float32(r.Min.Y)), Color1: rgb(a),
		Stop2: f32.Pt(float32(r.Max.X), float32(r.Max.Y)), Color2: rgb(b),
	}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	// A soft "sun" and "hills" so the placeholder reads as a photo.
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
	// Darken the bottom so a timestamp overlay stays readable.
	paint.LinearGradientOp{
		Stop1: f32.Pt(0, float32(r.Max.Y-gtx.Dp(40))), Color1: color.NRGBA{},
		Stop2: f32.Pt(0, float32(r.Max.Y)), Color2: color.NRGBA{A: 0x70},
	}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

func (u *UI) layoutComposer(gtx C) D {
	p := u.pal
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return background(gtx, p.Composer, 0, func(gtx C) D {
		return layout.Inset{Left: 10, Right: 10, Top: 5, Bottom: 5}.Layout(gtx, func(gtx C) D {
			hasText := trimSpace(u.conv.composer.Text()) != ""
			return layout.Flex{Alignment: layout.End}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Bottom: 6}.Layout(gtx, func(gtx C) D { return u.iconButton(gtx, &u.conv.emoji, icEmoji, p.Icon, false) })
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Bottom: 6}.Layout(gtx, func(gtx C) D { return u.iconButton(gtx, &u.conv.attach, icAttach, p.Icon, false) })
				}),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Flexed(1, func(gtx C) D {
					return layout.Inset{Top: 5, Bottom: 5}.Layout(gtx, func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						return background(gtx, p.Input, 8, func(gtx C) D {
							return layout.Inset{Left: 12, Right: 12, Top: 10, Bottom: 10}.Layout(gtx, func(gtx C) D {
								gtx.Constraints.Min.X = gtx.Constraints.Max.X
								gtx.Constraints.Max.Y = gtx.Dp(120)
								e := material.Editor(u.th, &u.conv.composer, "Type a message")
								e.TextSize = 15
								e.Color = p.Text
								e.HintColor = p.InputHint
								e.SelectionColor = argb(0x53bdeb, 0x60)
								return e.Layout(gtx)
							})
						})
					})
				}),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(func(gtx C) D {
					ic := icMic
					if hasText {
						ic = icSend
					}
					return layout.Inset{Bottom: 6}.Layout(gtx, func(gtx C) D { return u.iconButton(gtx, &u.conv.send, ic, p.Icon, false) })
				}),
			)
		})
	})
}

// layoutEmpty is the welcome pane shown when no chat is open.
func (u *UI) layoutEmpty(gtx C) D {
	p := u.pal
	dims := fill(gtx, p.EmptyBg)
	gtx.Constraints.Min = gtx.Constraints.Max
	layout.Center.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(460))
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				sz := gtx.Dp(150)
				ring := argb(0x25d366, 0x22)
				fillCircle(gtx, image.Pt(sz/2, sz/2), sz/2, ring)
				off := (sz - gtx.Dp(72)) / 2
				t := op.Offset(image.Pt(off, off)).Push(gtx.Ops)
				u.icons.layout(gtx, icLaptop, 72, p.Green)
				t.Pop()
				return D{Size: image.Pt(sz, sz)}
			}),
			layout.Rigid(layout.Spacer{Height: 28}.Layout),
			layout.Rigid(u.label(30, "WazzapClients for Windows", p.Text, labelOpts{weight: font.Light, maxLines: 1, align: text.Middle}).Layout),
			layout.Rigid(layout.Spacer{Height: 14}.Layout),
			layout.Rigid(u.label(14, "Send and receive messages without keeping your phone online. Native, lightweight, and no browser inside.",
				p.EmptyText, labelOpts{maxLines: 0, align: text.Middle}).Layout),
		)
	})
	layout.S.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = 0 // S only clears Min.Y
		return layout.Inset{Bottom: 36}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return u.icons.layout(gtx, icLock, 13, p.EmptyText) }),
				layout.Rigid(layout.Spacer{Width: 5}.Layout),
				layout.Rigid(u.label(13, "Your personal messages are end-to-end encrypted", p.EmptyText).Layout),
			)
		})
	})
	return dims
}
