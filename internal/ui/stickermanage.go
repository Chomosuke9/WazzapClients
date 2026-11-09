package ui

import (
	"image"
	"math"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Your stickers: the packs added to the sticker picker, like WhatsApp's
// list of them on phones. Each row shows a pack's name, who made it and
// its first stickers, a button that takes it off (asking first, in the
// row) and a handle that drags it to another place, which is the order of
// the picker's tabs. The picker's last tab opens it.

// myPacks is the Your stickers dialog's state.
type myPacks struct {
	packs    []*model.StickerPack
	removing string // the pack whose row asks before taking it off
	drag     packDrag
}

// packDrag is a row being dragged by its handle: from is its place when
// it was picked up, and dy how far the pointer (u.mouse) moved since.
type packDrag struct {
	on     bool
	id     string
	from   int
	startY int
	dy     int
}

// openMyPacks closes the picker and opens Your stickers.
func (u *UI) openMyPacks() {
	u.closePicker()
	u.dialog = dialogState{kind: dialogMyPacks, myPacks: myPacks{packs: u.backend.StickerPacks()}}
	u.dialog.list.Axis = layout.Vertical
}

// myPacksChanged reads the packs again after the backend's StickersEvent,
// unless a row is being dragged (the drop reads them).
func (u *UI) myPacksChanged() {
	if d := &u.dialog; d.kind == dialogMyPacks && !d.myPacks.drag.on {
		d.myPacks.packs = u.backend.StickerPacks()
	}
}

// packStickers returns an added pack's stickers, loading them on first
// use.
func (u *UI) packStickers(id string) []*model.Message {
	e := &u.picker
	if _, ok := e.packSt[id]; !ok {
		if e.packSt == nil {
			e.packSt = map[string][]*model.Message{}
		}
		e.packSt[id] = u.backend.PackStickers(id)
	}
	return e.packSt[id]
}

// myPacksDrag follows the dragged row: the pointer's movement while it's
// held, and on release the packs' new order, which goes to the backend.
// Rows glide to their places through u.follows ("mypk:<id>").
func (u *UI) myPacksDrag(gtx C, rowH int) {
	mp := &u.dialog.myPacks
	dr := &mp.drag
	for i, pk := range mp.packs {
		tag := u.btn("mypk:h:" + pk.ID)
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press | pointer.Release | pointer.Cancel})
			if !ok {
				break
			}
			e, ok := ev.(pointer.Event)
			if !ok {
				continue
			}
			switch {
			case e.Kind == pointer.Press && e.Buttons.Contain(pointer.ButtonPrimary) && !dr.on && u.dialog.isOpen():
				*dr = packDrag{on: true, id: pk.ID, from: i, startY: u.mouse.Y}
				mp.removing = ""
			case (e.Kind == pointer.Release || e.Kind == pointer.Cancel) && dr.on && dr.id == pk.ID:
				u.dropPack(rowH)
			}
		}
	}
	if !dr.on {
		return
	}
	if dr.from >= len(mp.packs) || mp.packs[dr.from].ID != dr.id {
		dr.on = false // the list changed under it
		return
	}
	dr.dy = u.mouse.Y - dr.startY
}

// packDragTarget is the place the dragged row would drop at.
func packDragTarget(dr packDrag, n, rowH int) int {
	return min(max(int(math.Round(float64(dr.from*rowH+dr.dy)/float64(rowH))), 0), n-1)
}

// packDragY is the dragged row's top in the list's content, kept inside it.
func packDragY(dr packDrag, n, rowH int) int {
	return min(max(dr.from*rowH+dr.dy, 0), (n-1)*rowH)
}

// packShift is how far row j moves aside while the dragged row hovers over
// place t.
func packShift(dr packDrag, j, t, rowH int) int {
	switch {
	case j == dr.from:
		return 0
	case dr.from < t && j > dr.from && j <= t:
		return -rowH
	case t < dr.from && j >= t && j < dr.from:
		return rowH
	}
	return 0
}

// dropPack puts the dragged row at its place and tells the backend. The
// rows keep where they are on screen and glide into their new slots.
func (u *UI) dropPack(rowH int) {
	mp := &u.dialog.myPacks
	dr := mp.drag
	mp.drag = packDrag{}
	n := len(mp.packs)
	if dr.from >= n {
		return
	}
	t := packDragTarget(dr, n, rowH)
	for j, pk := range mp.packs {
		key := "mypk:" + pk.ID
		if j == dr.from {
			u.follows.snap(key, float32(packDragY(dr, n, rowH)-t*rowH))
			continue
		}
		if sh := packShift(dr, j, t, rowH); sh != 0 {
			u.follows.snap(key, u.follows.value(key)-float32(sh))
		}
	}
	if t == dr.from {
		return
	}
	moved := mp.packs[dr.from]
	packs := append(mp.packs[:dr.from:dr.from], mp.packs[dr.from+1:]...)
	packs = append(packs[:t:t], append([]*model.StickerPack{moved}, packs[t:]...)...)
	mp.packs = packs
	ids := make([]string, len(packs))
	for i, pk := range packs {
		ids[i] = pk.ID
	}
	u.backend.SetStickerPackOrder(ids)
}

// myPacksPanel is the Your stickers dialog.
func (u *UI) myPacksPanel(gtx C) D {
	d := &u.dialog
	mp := &d.myPacks
	p := u.pal
	rowH := gtx.Dp(104)
	if u.btn("mypk:done").Clicked(gtx) && d.isOpen() {
		u.closeDialog()
	}
	if u.btn("mypk:keep").Clicked(gtx) {
		mp.removing = ""
	}
	if id := mp.removing; id != "" && u.btn("mypk:rm").Clicked(gtx) && d.isOpen() {
		mp.removing = ""
		u.backend.AddStickerPack(id, false)
		for i, pk := range mp.packs {
			if pk.ID == id {
				mp.packs = append(mp.packs[:i:i], mp.packs[i+1:]...)
				break
			}
		}
		u.toast("Removed from your stickers")
	}
	for _, pk := range mp.packs {
		if u.btn("mypk:del:"+pk.ID).Clicked(gtx) && d.isOpen() && !mp.drag.on {
			mp.removing = pk.ID
		}
	}
	u.myPacksDrag(gtx, rowH)

	w := min(gtx.Dp(520), gtx.Constraints.Max.X-gtx.Dp(32))
	maxH := gtx.Constraints.Max.Y - gtx.Dp(64)
	gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, maxH)}
	pad := gtx.Dp(24)
	inner := w - 2*pad

	tg := gtx
	tg.Constraints = layout.Constraints{Max: image.Pt(inner, 1<<20)}
	title := record(tg, u.label(19.5, "Your stickers", p.Text, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
	hint := "Sticker packs you add show here, and as tabs in the sticker picker."
	if len(mp.packs) > 1 {
		hint = "Drag a pack by its handle to move its tab in the sticker picker."
	}
	sub := record(tg, u.label(14, hint, p.TextSecondary, labelOpts{maxLines: 2}).Layout)
	btns := record(gtx, func(gtx C) D {
		gtx.Constraints.Min.X, gtx.Constraints.Max.X = inner, inner
		return layout.E.Layout(gtx, func(gtx C) D {
			return u.dialogButton(gtx, "mypk:done", dialogButton{label: "Done", primary: true})
		})
	})

	y := pad
	title.at(gtx, pad, y)
	y += title.size.Y + gtx.Dp(4)
	sub.at(gtx, pad, y)
	y += sub.size.Y + gtx.Dp(10)

	n := len(mp.packs)
	if n == 0 {
		m := record(gtx, func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(w, gtx.Dp(140)))
			return u.pickerMessage(gtx, "No sticker packs yet. Open one shared in a chat to add it.")
		})
		m.at(gtx, 0, y)
		y += m.size.Y
	} else {
		listH := min(n*rowH, max(rowH, maxH-y-btns.size.Y-gtx.Dp(16)-pad))
		dr := mp.drag
		t := -1
		if dr.on {
			t = packDragTarget(dr, n, rowH)
		}
		lg := gtx
		lg.Constraints = layout.Exact(image.Pt(w, listH))
		list := record(lg, func(gtx C) D {
			dims := u.scrollList(gtx, &d.list, n, func(gtx C, j int) D {
				pk := mp.packs[j]
				off := 0
				if dr.on {
					off = packShift(dr, j, t, rowH)
				}
				off = int(u.follows.step(gtx, "mypk:"+pk.ID, float32(off), durSlide))
				if !(dr.on && j == dr.from) {
					o := op.Offset(image.Pt(0, off)).Push(gtx.Ops)
					u.myPackRow(gtx, pk, w, rowH, false)
					o.Pop()
				}
				return D{Size: image.Pt(w, rowH)}
			})
			if dr.on {
				// The dragged row, lifted above the others.
				scroll := d.list.Position.First*rowH + d.list.Position.Offset
				ry := packDragY(dr, n, rowH) - scroll
				defer clip.Rect{Max: image.Pt(w, listH)}.Push(gtx.Ops).Pop()
				o := op.Offset(image.Pt(0, ry)).Push(gtx.Ops)
				r := image.Rect(gtx.Dp(8), 0, w-gtx.Dp(8), rowH)
				fillRRect(gtx, r.Add(image.Pt(0, gtx.Dp(2))).Inset(-gtx.Dp(2)), gtx.Dp(14), p.Shadow)
				fillRRect(gtx, r, gtx.Dp(12), p.Dialog)
				fillRRect(gtx, r, gtx.Dp(12), faded(p.Hover, 0.6))
				u.myPackRow(gtx, mp.packs[dr.from], w, rowH, true)
				o.Pop()
			}
			return dims
		})
		list.at(gtx, 0, y)
		y += list.size.Y
	}
	y += gtx.Dp(16)
	btns.at(gtx, pad, y)
	y += btns.size.Y + pad
	return D{Size: image.Pt(w, y)}
}

// myPackRow draws a pack's row: its name and maker, its first stickers,
// and the remove button and drag handle, or, when it asks before taking
// the pack off, the question and its two buttons.
func (u *UI) myPackRow(gtx C, pk *model.StickerPack, w, rowH int, lifted bool) {
	p := u.pal
	mp := &u.dialog.myPacks
	pad := gtx.Dp(24)
	box := gtx.Dp(40)
	asking := mp.removing == pk.ID && !lifted

	// The right side: the buttons, recorded first to know what's left.
	var right part
	if asking {
		right = record(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return u.dialogButton(gtx, "mypk:keep", dialogButton{label: "Cancel"}) }),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(func(gtx C) D { return u.dialogButton(gtx, "mypk:rm", dialogButton{label: "Remove", danger: true}) }),
			)
		})
	} else {
		right = record(gtx, func(gtx C) D {
			bg := gtx
			bg.Constraints = layout.Exact(image.Pt(box, box))
			if !lifted {
				u.iconButton(bg, u.btn("mypk:del:"+pk.ID), icDelete, 40, 24, p.Icon)
			} else {
				centerIn(bg, box, iconW(icDelete, 24, p.Icon))
			}
			o := op.Offset(image.Pt(box+gtx.Dp(4), 0)).Push(gtx.Ops)
			centerIn(bg, box, iconW(icDragHandle, 24, p.Icon))
			// The handle's own input area, which the drag starts from.
			// The dragged row's is the only one drawn, so its release comes.
			if lifted || !mp.drag.on {
				ar := clip.Rect{Max: image.Pt(box, box)}.Push(gtx.Ops)
				if lifted {
					pointer.CursorGrabbing.Add(gtx.Ops)
				} else {
					pointer.CursorGrab.Add(gtx.Ops)
				}
				event.Op(gtx.Ops, u.btn("mypk:h:"+pk.ID))
				ar.Pop()
			}
			o.Pop()
			return D{Size: image.Pt(2*box+gtx.Dp(4), box)}
		})
	}
	rx := w - pad + gtx.Dp(8) - right.size.X
	right.at(gtx, rx, (rowH-right.size.Y)/2)
	avail := rx - pad - gtx.Dp(12)

	// The name and, after it, who made the pack and its size.
	top := gtx.Dp(14)
	ng := gtx
	ng.Constraints = layout.Constraints{Max: image.Pt(max(0, avail), 1<<20)}
	head := record(ng, func(gtx C) D {
		return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
			layout.Rigid(u.label(16, first(pk.Name, pk.ID, "Sticker pack"), p.Text, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Flexed(1, func(gtx C) D {
				info := pk.Publisher
				if pk.Count > 0 {
					info = joinDot(info, packCount(pk.Count))
				}
				return u.label(14, info, p.TextSecondary, labelOpts{maxLines: 1}).Layout(gtx)
			}),
		)
	})
	head.at(gtx, pad, top)
	y := top + head.size.Y + gtx.Dp(8)

	if asking {
		qg := gtx
		qg.Constraints = layout.Constraints{Max: image.Pt(max(0, avail), 1<<20)}
		q := record(qg, u.label(14.5, "Remove this pack from your stickers?", p.TextSecondary, labelOpts{maxLines: 2}).Layout)
		q.at(gtx, pad, y+(gtx.Dp(52)-q.size.Y)/2)
		return
	}
	// Its first stickers, as many as fit (five at most).
	sz, gap := gtx.Dp(52), gtx.Dp(12)
	stickers := u.packStickers(pk.ID)
	fit := min(len(stickers), 5, max(0, (avail+gap)/(sz+gap)))
	for i := range fit {
		r := image.Rect(0, 0, sz, sz).Add(image.Pt(pad+i*(sz+gap), y))
		if img := u.messageImage(stickers[i], sz*2); img != nil && img.state == imgReady {
			paintStickerIn(gtx, img, r)
		} else {
			fillRRect(gtx, r, gtx.Dp(10), faded(p.TextSecondary, 0.08))
		}
	}
	if fit == 0 && len(stickers) == 0 {
		// No stickers stored: the tray icon stands for the pack.
		if img := u.packTray(pk.ID, sz*2); img != nil {
			paintStickerIn(gtx, img, image.Rect(0, 0, sz, sz).Add(image.Pt(pad, y)))
		}
	}
}
