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
// Each sticker keeps its own menu, reply and selection; in select mode a
// line's checkbox picks all of its stickers, and shows when only some are.
func (u *UI) layoutStickerRow(gtx C, c *model.Chat, r convRow, maxW, margin int) D {
	p := u.pal
	ms := r.group
	out := ms[0].FromMe
	w := gtx.Constraints.Max.X
	sel := u.conv.selecting
	selV := easeOut(u.conv.selV)
	shift := 0
	if !out {
		// Select mode moves incoming stickers over for the checkboxes.
		shift = int(float32(max(0, gtx.Dp(44)-margin)) * selV)
	}
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
	lines := stickerLines(len(ms), w-shift-hx, cell, gap)

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
		head.at(gtx, shift, 0)
	}
	if c.IsGroup && r.first && !out {
		// The sender's avatar sits in the left margin, level with the top.
		sz := gtx.Dp(29)
		t := op.Offset(image.Pt(shift-min(gtx.Dp(40), margin), 0)).Push(gtx.Ops)
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
		x0 := shift + hx
		if out {
			x0 = w - len(line)*cell - (len(line)-1)*gap
		}
		lt := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
		band := image.Rect(-margin, -gtx.Dp(2), w+margin, lh+gtx.Dp(2))
		if u.conv.flash != "" && (convRow{msg: line[0], group: line}).has(u.conv.flash) {
			u.drawFlash(gtx, band)
		}
		some := sel && u.pickedAny(line)
		if some {
			fillRect(gtx, band, argb(0x5dbf6e, 0x26))
		}
		if sel {
			// The line toggles all its stickers; each sticker, drawn on
			// top, toggles itself.
			t := op.Offset(image.Pt(-margin, 0)).Push(gtx.Ops)
			rg := gtx
			rg.Constraints = layout.Exact(image.Pt(w+2*margin, lh))
			clickable(rg, u.btn("srow:"+line[0].ID), func(gtx C) D { return D{Size: gtx.Constraints.Max} })
			t.Pop()
		}
		for i, m := range line {
			x := x0 + i*(cell+gap)
			u.stickerCell(gtx, c, m, parts[i], image.Pt(x, 0), sel, selV)
		}
		if selV > 0 {
			box, col := icCheckBoxEmpty, p.TextSecondary
			switch {
			case u.pickedAll(line):
				box, col = icCheckBox, p.Green
			case some:
				box, col = icCheckBoxSome, p.Green
			}
			bt := op.Offset(image.Pt(-margin+gtx.Dp(12), gtx.Dp(6))).Push(gtx.Ops)
			withOpacity(gtx, selV, func() { drawIcon(gtx, box, 24, col) })
			bt.Pop()
		}
		lt.Pop()
		y += lh + lineGap
	}
	return D{Size: image.Pt(w, y-lineGap)}
}

// stickerCell draws one sticker of a run at at, with its right-click menu
// and double-click reply, or in select mode its own checkbox.
func (u *UI) stickerCell(gtx C, c *model.Chat, m *model.Message, pt part, at image.Point, sel bool, selV float32) {
	p := u.pal
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
	if selV > 0 {
		box, col := icCheckBoxEmpty, p.TextSecondary
		if u.conv.picked[m.ID] {
			box, col = icCheckBox, p.Green
		}
		bt := op.Offset(image.Pt(gtx.Dp(150-22), gtx.Dp(2))).Push(gtx.Ops)
		withOpacity(gtx, selV, func() { drawIcon(gtx, box, 20, col) })
		bt.Pop()
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
