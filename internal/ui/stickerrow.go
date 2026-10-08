package ui

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// groupsSticker reports whether m can sit beside the stickers sent just
// before it: a sticker that doesn't reply to anything (one that does sits
// in a bubble).
func groupsSticker(m *model.Message) bool {
	return m.Kind == model.KindSticker && m.Quote == nil
}

// stickerGap is the room between two stickers side by side.
const stickerGap = 24

// stickerLines splits a run of n stickers into lines of as many as fit in
// w px, each cell px wide.
func stickerLines(n, w, cell, gap int) [][2]int {
	per := max(1, (w+gap)/(cell+gap))
	var lines [][2]int
	for i := 0; i < n; i += per {
		lines = append(lines, [2]int{i, min(n, i+per)})
	}
	return lines
}

// layoutStickerRow draws a run of stickers from one sender side by side,
// as many to a line as the chat is wide (WhatsApp puts two on a line).
// Each sticker keeps its own menu, reply and selection; in select mode
// clicking beside a line picks all of its stickers, and a line with only
// some picked highlights those alone.
func (u *UI) layoutStickerRow(gtx C, c *model.Chat, r convRow, maxW, margin int) D {
	ms := r.group
	out := ms[0].FromMe
	w := gtx.Constraints.Max.X
	sel := u.conv.selecting
	cell, gap := gtx.Dp(150), gtx.Dp(stickerGap)
	// In a group, the run's first line is headed by the sender's name and
	// moves over under it, as a lone sticker does.
	var head part
	hx, y := 0, 0
	hasHead := c.IsGroup && r.first && !out && ms[0].Sender != ""
	if hasHead {
		head = u.stickerHeader(gtx, ms[0], cell, maxW)
		hx, y = stickerUnderHeader(gtx, head, cell)
	}
	lines := stickerLines(len(ms), w-hx, cell, gap)

	if sel {
		for _, l := range lines {
			line := ms[l[0]:l[1]]
			if u.btn("srow:" + line[0].ID).Clicked(gtx) {
				all := u.pickedAll(line)
				for _, m := range line {
					if all {
						delete(u.conv.picked, m.ID)
					} else {
						u.conv.picked[m.ID] = true
					}
				}
			}
			for _, m := range line {
				if u.btn("row:" + m.ID).Clicked(gtx) {
					if u.conv.picked[m.ID] {
						delete(u.conv.picked, m.ID)
					} else {
						u.conv.picked[m.ID] = true
					}
				}
			}
		}
	}

	if hasHead {
		head.at(gtx, 0, 0)
	}
	if c.IsGroup && r.first && !out {
		// The sender's avatar sits in the left margin, level with the top.
		sz := gtx.Dp(29)
		t := op.Offset(image.Pt(-min(gtx.Dp(40), margin), 0)).Push(gtx.Ops)
		u.avatar(gtx, ms[0].SenderID, ms[0].Sender, false, dp(gtx, sz))
		u.senderButton(gtx, ms[0], image.Point{}, image.Pt(sz, sz))
		t.Pop()
	}
	lineGap := gtx.Dp(8)
	for _, l := range lines {
		line := ms[l[0]:l[1]]
		parts := make([]part, len(line))
		lh := 0
		for i, m := range line {
			cgtx := gtx
			cgtx.Constraints = layout.Constraints{Max: image.Pt(cell, gtx.Constraints.Max.Y)}
			parts[i] = record(cgtx, func(gtx C) D {
				return u.layoutMessage(gtx, c, convRow{kind: rowMessage, msg: m}, maxW)
			})
			lh = max(lh, parts[i].size.Y)
		}
		x0 := hx
		if out {
			x0 = w - len(line)*cell - (len(line)-1)*gap
		}
		lt := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
		band := image.Rect(-margin, -gtx.Dp(2), w+margin, lh+gtx.Dp(2))
		if u.conv.flash != "" && (convRow{msg: line[0], group: line}).has(u.conv.flash) {
			u.drawFlash(gtx, band)
		}
		// Like a row, a line's band reaches halfway to its neighbours.
		sb := image.Rect(-margin, -(lineGap - lineGap/2), w+margin, lh+lineGap/2)
		if l == lines[0] {
			sb.Min.Y = -u.conv.band[0]
		}
		if l == lines[len(lines)-1] {
			sb.Max.Y = lh + u.conv.band[1]
		}
		all := sel && u.pickedAll(line)
		if all {
			fillRect(gtx, sb, selColor)
		}
		if sel {
			// The line toggles all its stickers; each sticker, drawn on
			// top, toggles itself.
			t := op.Offset(sb.Min).Push(gtx.Ops)
			rg := gtx
			rg.Constraints = layout.Exact(sb.Size())
			clickable(rg, u.btn("srow:"+line[0].ID), func(gtx C) D { return D{Size: gtx.Constraints.Max} })
			t.Pop()
		}
		for i, m := range line {
			x := x0 + i*(cell+gap)
			if sel && !all && u.conv.picked[m.ID] {
				fillRRect(gtx, image.Rectangle{Min: image.Pt(x, 0), Max: image.Pt(x+cell, lh)}.Inset(-gtx.Dp(4)), gtx.Dp(8), selColor)
			}
			u.stickerCell(gtx, c, m, parts[i], image.Pt(x, 0), sel)
		}
		lt.Pop()
		y += lh + lineGap
	}
	return D{Size: image.Pt(w, y-lineGap)}
}

// stickerCell draws one sticker of a run at at, with its right-click menu
// and double-click reply, or in select mode a click that picks it.
func (u *UI) stickerCell(gtx C, c *model.Chat, m *model.Message, pt part, at image.Point, sel bool) {
	defer op.Offset(at).Push(gtx.Ops).Pop()
	// A sticker that just arrived pops in where it joins the run.
	fx := fxStack{}
	if k := (animKey{id: m.ID, tag: tagAppear}); u.anims.running(k) {
		v := u.anims.fade(gtx, k, true, durAppear, durAppear)
		if v >= 1 {
			u.anims.stop(k)
		}
		if u.conv.takeover == m.ID {
			u.conv.takeover = ""
		}
		mid := image.Pt(pt.size.X/2, pt.size.Y/2)
		fx = pushFx(gtx, easeOut(v), scaleAt(mid, lerp(0.6, 1, easeOutBack(v))))
	}
	pt.at(gtx, 0, 0)
	fx.Pop()
	if sel {
		cg := gtx
		cg.Constraints = layout.Exact(pt.size)
		clickable(cg, u.btn("row:"+m.ID), func(gtx C) D { return D{Size: gtx.Constraints.Max} })
	} else {
		right, _, double := u.pressArea(gtx, m.ID, image.Rectangle{Max: pt.size})
		if right {
			u.openMessageMenu(m)
		}
		if double && m.Kind != model.KindDeleted && m.Revoked.IsZero() && u.sendBlocked(c) == "" {
			u.startReply(m)
		}
	}
}

// pickedAll reports whether every message of ms is picked in select mode.
func (u *UI) pickedAll(ms []*model.Message) bool {
	for _, m := range ms {
		if !u.conv.picked[m.ID] {
			return false
		}
	}
	return true
}

// pickedAny reports whether any message of ms is picked in select mode.
func (u *UI) pickedAny(ms []*model.Message) bool {
	for _, m := range ms {
		if u.conv.picked[m.ID] {
			return true
		}
	}
	return false
}
