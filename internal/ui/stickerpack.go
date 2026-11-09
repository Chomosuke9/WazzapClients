package ui

import (
	"fmt"
	"image"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Sticker packs: a shared pack is a card in its bubble (its picture, name
// and how many stickers it has) with a "View sticker pack" button, which
// a link to one of WhatsApp's own packs (wa.me/stickerpack/<id>) gets
// under its link preview too. The button opens the pack in a dialog: its
// stickers, which send to the open chat, and a button that adds it to the
// sticker picker, where it is a tab of its own.

// packChat is the backend's pseudo-chat of pack tray icons (wa.packChat).
const packChat = "stickerpack"

// packView is the sticker pack dialog's state.
type packView struct {
	msg      *model.Message // the message that shares it
	pack     *model.StickerPack
	stickers []*model.Message
	err      string
}

// openPackView opens the pack m shares and asks the backend for it.
func (u *UI) openPackView(m *model.Message) {
	if m.Pack == nil {
		return
	}
	cp := *m.Pack
	u.dialog = dialogState{kind: dialogPack, pack: packView{msg: m, pack: &cp}}
	u.dialog.list.Axis = layout.Vertical
	u.backend.OpenStickerPack(m)
}

// packLoaded fills the dialog with the pack the backend loaded.
func (u *UI) packLoaded(e model.StickerPackEvent) {
	d := &u.dialog
	if d.kind != dialogPack || d.pack.msg == nil || d.pack.msg.Pack.ID != e.ID {
		return
	}
	if e.Err != "" {
		d.pack.err = e.Err
		return
	}
	d.pack.pack, d.pack.stickers, d.pack.err = e.Pack, e.Stickers, ""
}

// packCount is "3 stickers".
func packCount(n int) string {
	if n == 1 {
		return "1 sticker"
	}
	return fmt.Sprintf("%d stickers", n)
}

// layoutPackCard draws a shared sticker pack: its picture, name and size.
func (u *UI) layoutPackCard(gtx C, m *model.Message, maxW, metaH int, cols bubbleColors) D {
	pk := m.Pack
	btn := u.btn("packcard:" + m.ID)
	if btn.Clicked(gtx) {
		u.openPackView(m)
	}
	w := min(maxW, gtx.Dp(300))
	sz := gtx.Dp(88)
	card := image.Rect(0, 0, w, sz)
	cg := gtx
	cg.Constraints = layout.Exact(card.Size())
	clickable(cg, btn, func(gtx C) D {
		fillRRect(gtx, card, gtx.Dp(6), cols.card)
		if h := u.hover(gtx, btn); h > 0 {
			fillRRect(gtx, card, gtx.Dp(6), faded(rgb(0x000000), 0.06*h))
		}
		return D{Size: card.Size()}
	})
	pic := image.Rect(0, 0, sz, sz)
	b, chat, id := u.backend, m.ChatID, m.ID
	if img := u.images.get("m:"+chat+"/"+id, sz*2, func() []byte { return b.MediaData(chat, id) }); img.state == imgReady && !u.blurred() {
		paintRRectImage(gtx, img, pic, gtx.Dp(6))
	} else {
		fillRRect(gtx, pic, gtx.Dp(6), faded(cols.secondary, 0.15))
		isz := gtx.Dp(36)
		t := op.Offset(image.Pt((sz-isz)/2, (sz-isz)/2)).Push(gtx.Ops)
		drawIcon(gtx, icSticker, 36, cols.secondary)
		t.Pop()
	}
	tx := sz + gtx.Dp(12)
	lg := gtx
	lg.Constraints = layout.Constraints{Max: image.Pt(max(0, w-tx-gtx.Dp(8)), 1<<20)}
	nm := record(lg, u.label(15.5, first(pk.Name, "Sticker pack"), cols.text, labelOpts{weight: font.SemiBold, maxLines: 2}).Layout)
	sub := packCount(pk.Count)
	if pk.Count == 0 {
		sub = "Sticker pack"
	}
	ct := record(lg, u.label(13.5, sub, cols.secondary, labelOpts{maxLines: 1}).Layout)
	y := (sz - nm.size.Y - ct.size.Y - gtx.Dp(2)) / 2
	nm.at(gtx, tx, max(0, y))
	ct.at(gtx, tx, max(0, y)+nm.size.Y+gtx.Dp(2))
	return D{Size: image.Pt(w, sz+gtx.Dp(2)+metaH)}
}

// paintRRectImage paints a picture covering r with rounded corners.
func paintRRectImage(gtx C, img *imgEntry, r image.Rectangle, radius int) {
	defer clip.UniformRRect(r, radius).Push(gtx.Ops).Pop()
	paintCover(gtx, img.op, img.size, r)
}

// packPanel is the sticker pack dialog: the pack, its stickers and a
// button that adds it to the picker or takes it off.
func (u *UI) packPanel(gtx C) D {
	d := &u.dialog
	pv := &d.pack
	p := u.pal
	pk := pv.pack
	loaded := pv.stickers != nil
	if u.btn("pack:close").Clicked(gtx) && d.isOpen() {
		u.closeDialog()
	}
	if u.btn("pack:add").Clicked(gtx) && d.isOpen() && loaded {
		add := !pk.Added
		u.backend.AddStickerPack(pk.ID, add)
		pk.Added = add
		if add {
			u.toast("Added to your stickers")
		} else {
			u.toast("Removed from your stickers")
		}
	}
	for _, s := range pv.stickers {
		if u.btn("packst:"+s.ID).Clicked(gtx) && d.isOpen() && u.selected != nil {
			u.backend.SendSticker(u.selected.ID, s, u.conv.reply)
			u.conv.reply = nil
			u.closeDialog()
		}
	}

	w := min(gtx.Dp(460), gtx.Constraints.Max.X-gtx.Dp(32))
	maxH := gtx.Constraints.Max.Y - gtx.Dp(64)
	gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, maxH)}
	pad := gtx.Dp(24)
	inner := w - 2*pad

	// The head: the tray icon, the name, who made it and its size.
	tray := gtx.Dp(56)
	hg := gtx
	hg.Constraints = layout.Constraints{Max: image.Pt(inner-tray-gtx.Dp(14), 1<<20)}
	name := record(hg, u.label(19.5, first(pk.Name, pk.ID, "Sticker pack"), p.Text, labelOpts{weight: font.Medium, maxLines: 2}).Layout)
	info := pk.Publisher
	if pk.Count > 0 {
		info = joinDot(info, packCount(pk.Count))
	}
	if pk.Size > 0 {
		info = joinDot(info, sizeLabel(pk.Size))
	}
	sub := record(hg, u.label(14, info, p.TextSecondary, labelOpts{maxLines: 2}).Layout)
	headH := max(tray, name.size.Y+sub.size.Y+gtx.Dp(2))
	var about part
	if pk.About != "" {
		ag := gtx
		ag.Constraints = layout.Constraints{Max: image.Pt(inner, 1<<20)}
		about = record(ag, u.label(14, pk.About, p.TextSecondary, labelOpts{maxLines: 3}).Layout)
	}
	btns := record(gtx, func(gtx C) D {
		gtx.Constraints.Min.X, gtx.Constraints.Max.X = inner, inner
		return layout.E.Layout(gtx, func(gtx C) D {
			add := dialogButton{label: "Add to stickers", primary: true, disabled: !loaded}
			if pk.Added {
				add = dialogButton{label: "Remove from stickers", disabled: !loaded}
			}
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return u.dialogButton(gtx, "pack:close", dialogButton{label: "Close"}) }),
				layout.Rigid(layout.Spacer{Width: 12}.Layout),
				layout.Rigid(func(gtx C) D { return u.dialogButton(gtx, "pack:add", add) }),
			)
		})
	})

	y := pad
	t := op.Offset(image.Pt(pad, y)).Push(gtx.Ops)
	tr := image.Rect(0, 0, tray, tray)
	if img := u.packTray(pk.ID, tray*2); img != nil {
		paintStickerIn(gtx, img, tr)
	} else {
		fillRRect(gtx, tr, gtx.Dp(10), p.Hover)
		isz := gtx.Dp(30)
		it := op.Offset(image.Pt((tray-isz)/2, (tray-isz)/2)).Push(gtx.Ops)
		drawIcon(gtx, icSticker, 30, p.Icon)
		it.Pop()
	}
	t.Pop()
	ny := y + (headH-name.size.Y-sub.size.Y-gtx.Dp(2))/2
	name.at(gtx, pad+tray+gtx.Dp(14), ny)
	sub.at(gtx, pad+tray+gtx.Dp(14), ny+name.size.Y+gtx.Dp(2))
	y += headH
	if pk.About != "" {
		y += gtx.Dp(12)
		about.at(gtx, pad, y)
		y += about.size.Y
	}
	y += gtx.Dp(14)

	// The stickers, four to a row, or what's keeping them.
	gridH := max(gtx.Dp(120), maxH-y-btns.size.Y-gtx.Dp(18)-pad)
	switch {
	case pv.err != "":
		m := record(gtx, func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(w, gtx.Dp(120)))
			return u.pickerMessage(gtx, pv.err)
		})
		m.at(gtx, 0, y)
		y += m.size.Y
	case !loaded:
		m := record(gtx, func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(w, gtx.Dp(120)))
			return u.pickerMessage(gtx, "Loading stickers…")
		})
		m.at(gtx, 0, y)
		y += m.size.Y
	default:
		cols := 4
		cell := (inner + gtx.Dp(16)) / cols
		rows := (len(pv.stickers) + cols - 1) / cols
		gridH = min(gridH, rows*cell)
		lg := gtx
		lg.Constraints = layout.Exact(image.Pt(w, gridH))
		grid := record(lg, func(gtx C) D {
			return u.scrollList(gtx, &d.list, rows, func(gtx C, row int) D {
				for i := row * cols; i < min((row+1)*cols, len(pv.stickers)); i++ {
					s := pv.stickers[i]
					ct := op.Offset(image.Pt(pad-gtx.Dp(8)+(i-row*cols)*cell, 0)).Push(gtx.Ops)
					cg := gtx
					cg.Constraints = layout.Exact(image.Pt(cell, cell))
					cl := u.btn("packst:" + s.ID)
					clickable(cg, cl, func(gtx C) D {
						if h := u.hover(gtx, cl); h > 0 {
							fillRRect(gtx, image.Rect(0, 0, cell, cell), gtx.Dp(10), faded(p.PopupHover, h))
						}
						if img := u.messageImage(s, cell*2); img != nil && img.state == imgReady {
							pd := gtx.Dp(8)
							paintStickerIn(gtx, img, image.Rect(pd, pd, cell-pd, cell-pd))
						} else {
							fillRRect(gtx, image.Rect(gtx.Dp(8), gtx.Dp(8), cell-gtx.Dp(8), cell-gtx.Dp(8)), gtx.Dp(10), faded(p.TextSecondary, 0.08))
						}
						return D{Size: image.Pt(cell, cell)}
					})
					ct.Pop()
				}
				return D{Size: image.Pt(w, cell)}
			})
		})
		grid.at(gtx, 0, y)
		y += grid.size.Y
	}
	y += gtx.Dp(18)
	btns.at(gtx, pad, y)
	y += btns.size.Y + pad
	return D{Size: image.Pt(w, y)}
}

// joinDot joins two details with " · ", leaving out empty ones.
func joinDot(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + " · " + b
}

// paintStickerIn draws a sticker's picture in r, keeping its aspect ratio.
func paintStickerIn(gtx C, img *imgEntry, r image.Rectangle) {
	sc := min(float32(r.Dx())/float32(img.size.X), float32(r.Dy())/float32(img.size.Y))
	iw, ih := int(float32(img.size.X)*sc), int(float32(img.size.Y)*sc)
	min := r.Min.Add(image.Pt((r.Dx()-iw)/2, (r.Dy()-ih)/2))
	paintCover(gtx, img.op, img.size, image.Rectangle{Min: min, Max: min.Add(image.Pt(iw, ih))})
}

// packTray returns a pack's tray icon, or nil while it loads (or when it
// has none).
func (u *UI) packTray(id string, px int) *imgEntry {
	b := u.backend
	img := u.images.get("tray:"+id, px, func() []byte { return b.MediaData(packChat, id) })
	if img.state != imgReady {
		return nil
	}
	return img
}

// pickerPacks returns the packs added to the picker, loading them on
// first use. A pack's tab that was open stays open where the pack is now.
func (u *UI) pickerPacks() []*model.StickerPack {
	e := &u.picker
	if !e.packsOK {
		var open string
		if i := int(e.stickerSet) - len(stickerSets); i >= 0 && i < len(e.packs) {
			open = e.packs[i].ID
		}
		e.packs, e.packSt, e.packsOK = u.backend.StickerPacks(), nil, true
		if open != "" {
			e.stickerSet = model.StickerSet(len(stickerSets) + len(e.packs)) // gone, unless found
			for i, p := range e.packs {
				if p.ID == open {
					e.stickerSet = model.StickerSet(len(stickerSets) + i)
				}
			}
		}
	}
	return e.packs
}

// stickerTabs is the sticker tab's header: recent, favourite and received
// stickers, then a tab for each pack added, shown by its tray icon. The
// tabs keep the emoji categories' pitch; when they don't fit, the wheel
// scrolls them sideways.
func (u *UI) stickerTabs(gtx C) D {
	e := &u.picker
	p := u.pal
	packs := u.pickerPacks()
	n := len(stickerSets) + len(packs)
	if u.btn("stmanage").Clicked(gtx) {
		u.openMyPacks()
	}
	if len(packs) > 0 {
		n++ // the last opens Your stickers
	}
	w := gtx.Constraints.Max.X
	gtx.Constraints.Min.X = w
	tabW := w / len(emojiCategories)
	h := gtx.Dp(57)
	maxScroll := max(0, n*tabW-w)
	tag := &e.tabScroll
	rng := pointer.ScrollRange{Min: -e.tabScroll, Max: maxScroll - e.tabScroll}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Scroll, ScrollX: rng, ScrollY: rng})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok && pe.Kind == pointer.Scroll {
			e.tabScroll += int(pe.Scroll.X + pe.Scroll.Y)
		}
	}
	e.tabScroll = min(max(e.tabScroll, 0), maxScroll)
	area := clip.Rect(image.Rect(0, 0, w, h)).Push(gtx.Ops)
	event.Op(gtx.Ops, tag)
	for i := range n {
		x := i*tabW - e.tabScroll
		if x+tabW <= 0 || x >= w {
			continue
		}
		t := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		cg := gtx
		cg.Constraints = layout.Exact(image.Pt(tabW, h))
		cl := u.btn("stset:" + itoa(i+1))
		manage := len(packs) > 0 && i == n-1
		if manage {
			cl = u.btn("stmanage")
		}
		clickable(cg, cl, func(gtx C) D {
			if i < len(stickerSets) || manage {
				ic := icSettings
				if !manage {
					ic = stickerSets[i].ic
				}
				sel := float32(0)
				if !manage {
					sel = e.stickerSel.of(i)
				}
				isz := gtx.Dp(26)
				icT := op.Offset(image.Pt((tabW-isz)/2, gtx.Dp(29)-isz/2)).Push(gtx.Ops)
				// Icon colors are cached per color: cross-fade two icons
				// instead of animating the color.
				drawIcon(gtx, ic, 26, p.Icon)
				withOpacity(gtx, max(u.hover(gtx, cl), sel), func() { drawIcon(gtx, ic, 26, p.Text) })
				icT.Pop()
				return D{Size: gtx.Constraints.Max}
			}
			pk := packs[i-len(stickerSets)]
			isz := gtx.Dp(32)
			r := image.Rect((tabW-isz)/2, gtx.Dp(29)-isz/2, (tabW+isz)/2, gtx.Dp(29)+isz/2)
			if hv := u.hover(gtx, cl); hv > 0 {
				fillRRect(gtx, r.Inset(-gtx.Dp(4)), gtx.Dp(8), faded(p.PopupHover, hv))
			}
			if img := u.packTray(pk.ID, isz*2); img != nil {
				paintStickerIn(gtx, img, r)
			} else {
				icT := op.Offset(image.Pt((tabW-gtx.Dp(26))/2, gtx.Dp(29)-gtx.Dp(13))).Push(gtx.Ops)
				drawIcon(gtx, icSticker, 26, p.Icon)
				icT.Pop()
			}
			return D{Size: gtx.Constraints.Max}
		})
		t.Pop()
	}
	x := int(e.stickerLine.step(gtx, float32(int(e.stickerSet)*tabW), durSlide)) - e.tabScroll
	bw := gtx.Dp(27)
	fillRRect(gtx, image.Rect(x+(tabW-bw)/2, gtx.Dp(49), x+(tabW+bw)/2, gtx.Dp(52)), gtx.Dp(2), p.Green)
	area.Pop()
	return D{Size: image.Pt(w, h)}
}
