package ui

import (
	"image"
	"strconv"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// An album is photos and videos sent together (model.Message.Album). Its
// pictures without a caption show as one bubble, a grid of up to four
// tiles; from five on, the fourth is covered by "+N" for it and the rest,
// and the viewer pages through all of them. Each picture keeps its own menu,
// reply and selection, from its tile.

// albumMember reports whether m shows in its album's grid. A picture with
// a caption shows on its own, as in WhatsApp.
func albumMember(m *model.Message) bool {
	return m.Album != "" && m.Kind == model.KindImage && m.Text == ""
}

// joinsAlbum reports whether m goes in the same grid as prev, the picture
// before it. Only the first picture of a grid may reply to something.
func joinsAlbum(prev, m *model.Message) bool {
	return albumMember(prev) && albumMember(m) && m.Album == prev.Album && m.Quote == nil &&
		m.FromMe == prev.FromMe && m.SenderID == prev.SenderID && m.Sender == prev.Sender
}

// albumFile reports whether a picked file goes in an album with the
// others sent with it: photos and videos, unless they're view once.
func albumFile(f *attachFile) bool {
	return (f.Media == model.MediaImage || f.Media == model.MediaVideo) && !f.ViewOnce
}

// openAlbum opens an album for the photos and videos among files, when
// there are two or more, and puts them in it.
func (u *UI) openAlbum(chatID string, files []*attachFile) {
	photos, videos := 0, 0
	for _, f := range files {
		switch {
		case !albumFile(f):
		case f.Media == model.MediaImage:
			photos++
		default:
			videos++
		}
	}
	if photos+videos < 2 {
		return
	}
	id := u.backend.NewAlbum(chatID, photos, videos)
	if id == "" {
		return // they go one by one
	}
	for _, f := range files {
		if albumFile(f) {
			f.Album = id
		}
	}
}

// albumTiles places an album's n pictures in a grid w px wide, gap px
// apart: two side by side, three as one above two, four or more two by
// two. It returns a tile for each of the first four.
func albumTiles(n, w, gap int) []image.Rectangle {
	half := (w - gap) / 2
	r := image.Rect
	switch {
	case n <= 1:
		return []image.Rectangle{r(0, 0, w, w)}
	case n == 2:
		return []image.Rectangle{r(0, 0, half, half), r(w-half, 0, w, half)}
	case n == 3:
		y := half + gap
		return []image.Rectangle{r(0, 0, w, half), r(0, y, half, y+half), r(w-half, y, w, y+half)}
	}
	y := half + gap
	return []image.Rectangle{r(0, 0, half, half), r(w-half, 0, w, half), r(0, y, half, y+half), r(w-half, y, w, y+half)}
}

// albumMeta is the message whose time and ticks an album's grid shows:
// its last picture's time, with the ticks of the least far along of them.
func albumMeta(ms []*model.Message) *model.Message {
	meta := *ms[len(ms)-1]
	meta.Starred = false
	for _, m := range ms {
		meta.Receipt = min(meta.Receipt, m.Receipt)
		meta.Starred = meta.Starred || m.Starred
	}
	return &meta
}

// albumReactions joins the different reactions to an album's pictures.
func albumReactions(ms []*model.Message) string {
	var s string
	seen := map[string]bool{}
	for _, m := range ms {
		if m.Reaction != "" && !seen[m.Reaction] {
			seen[m.Reaction] = true
			s += m.Reaction
		}
	}
	return s
}

// layoutAlbumRow draws an album's row: its bubble, the sender's avatar in
// groups, a chevron on the hovered tile, and in select mode a checkbox
// that picks all its pictures.
func (u *UI) layoutAlbumRow(gtx C, c *model.Chat, r convRow, maxW, margin int) D {
	p := u.pal
	ms := r.group
	m0 := ms[0]
	out := m0.FromMe
	w := gtx.Constraints.Max.X
	sel := u.conv.selecting
	rowKey := "arow:" + m0.ID
	if sel && u.btn(rowKey).Clicked(gtx) {
		all := u.pickedAll(ms)
		for _, m := range ms {
			if all {
				delete(u.conv.picked, m.ID)
			} else {
				u.conv.picked[m.ID] = true
			}
		}
	}
	// Select mode moves incoming bubbles over for the checkboxes.
	selV := easeOut(u.conv.selV)
	shift := 0
	if !out {
		shift = int(float32(max(0, gtx.Dp(44)-margin)) * selV)
	}
	cgtx := gtx
	cgtx.Constraints = layout.Constraints{Max: image.Pt(w-shift, gtx.Constraints.Max.Y)}
	var tiles []image.Rectangle // in the bubble
	bubble := record(cgtx, func(gtx C) D {
		var dims D
		dims, tiles = u.layoutAlbum(gtx, c, r, maxW)
		return dims
	})
	x := shift
	if out {
		x = w - bubble.size.X
	}
	h := bubble.size.Y
	band := image.Rect(-margin, -gtx.Dp(2), w+margin, h+gtx.Dp(2))
	if u.conv.flash != "" && r.has(u.conv.flash) {
		u.drawFlash(gtx, band)
	}
	some := sel && u.pickedAny(ms)
	if some {
		fillRect(gtx, band, argb(0x5dbf6e, 0x26))
	}
	bubble.at(gtx, x, 0)
	if c.IsGroup && r.first && !out {
		// The sender's avatar sits in the left margin, level with the bubble.
		sz := gtx.Dp(29)
		t := op.Offset(image.Pt(x-min(gtx.Dp(40), margin), 0)).Push(gtx.Ops)
		u.avatar(gtx, m0.SenderID, m0.Sender, false, dp(gtx, sz))
		u.senderButton(gtx, m0, image.Point{}, image.Pt(sz, sz))
		t.Pop()
	}
	if !sel {
		for i, tr := range tiles {
			u.albumTile(gtx, c, ms[i], tr.Add(image.Pt(x, 0)))
		}
	}
	if selV > 0 {
		t := op.Offset(image.Pt(-margin, 0)).Push(gtx.Ops)
		if sel {
			rg := gtx
			rg.Constraints = layout.Exact(image.Pt(w+2*margin, h))
			clickable(rg, u.btn(rowKey), func(gtx C) D { return D{Size: gtx.Constraints.Max} })
		}
		box, col := icCheckBoxEmpty, p.TextSecondary
		switch {
		case u.pickedAll(ms):
			box, col = icCheckBox, p.Green
		case some:
			box, col = icCheckBoxSome, p.Green
		}
		bt := op.Offset(image.Pt(gtx.Dp(12), gtx.Dp(6))).Push(gtx.Ops)
		withOpacity(gtx, selV, func() { drawIcon(gtx, box, 24, col) })
		bt.Pop()
		t.Pop()
	}
	return D{Size: image.Pt(w, h)}
}

// albumTile handles one tile of an album, at tr: right click opens its
// picture's menu, a double click replies to it, and hovering shows its
// chevron.
func (u *UI) albumTile(gtx C, c *model.Chat, m *model.Message, tr image.Rectangle) {
	defer op.Offset(tr.Min).Push(gtx.Ops).Pop()
	sz := tr.Size()
	hovered := u.hoverArea(gtx, m.ID, sz)
	right, _, double := u.pressArea(gtx, m.ID, image.Rectangle{Max: sz})
	if right {
		u.openMessageMenu(m)
	}
	if double && u.sendBlocked(c) == "" {
		u.startReply(m)
	}
	chev := u.btn("chev:" + m.ID)
	if chev.Clicked(gtx) {
		u.openMessageMenu(m)
	}
	show := hovered || chev.Hovered() || (u.ctx.isOpen() && u.ctx.msg == m)
	if cv := smooth(u.anims.fade(gtx, animKey{p: chev, tag: tagShow}, show, durHoverIn, durHoverOut)); cv > 0 {
		ct := op.Offset(image.Pt(sz.X-gtx.Dp(26+5), gtx.Dp(4))).Push(gtx.Ops)
		fx := pushFx(gtx, cv, moveBy(float32(gtx.Dp(6))*(1-cv), 0))
		u.chevronButton(gtx, chev, argb(0x000000, 0x60), rgb(0xffffff))
		fx.Pop()
		ct.Pop()
	}
}

// layoutAlbum draws an album's bubble, sized to its grid, and returns
// where its tiles are in it.
func (u *UI) layoutAlbum(gtx C, c *model.Chat, r convRow, maxW int) (D, []image.Rectangle) {
	p := u.pal
	ms := r.group
	m0 := ms[0]
	out := m0.FromMe
	bg, quoteBg, secondary := p.BubbleIn, p.QuoteIn, p.TextSecondary
	if out {
		bg, quoteBg, secondary = p.BubbleOut, p.QuoteOut, p.SecondaryOut
	}
	pad := gtx.Dp(3)
	gridW := min(maxW-2*pad, gtx.Dp(330))
	textInset := gtx.Dp(6)
	cgtx := gtx
	cgtx.Constraints = layout.Constraints{Max: image.Pt(gridW, 1<<20)}
	tiles := albumTiles(len(ms), gridW, gtx.Dp(3))
	for i := range tiles {
		if u.btn("img:" + ms[i].ID).Clicked(gtx) {
			u.openViewer(ms[i])
		}
	}
	if u.btn("quote:"+m0.ID).Clicked(gtx) && m0.Quote != nil && m0.Quote.ID != "" {
		u.jumpTo(m0.Quote.ID)
	}

	white := rgb(0xffffff)
	meta := record(cgtx, func(gtx C) D { return u.layoutMeta(gtx, albumMeta(ms), white, &white) })
	var sender, fwd, quote part
	hasSender := c.IsGroup && !out && r.first && m0.Sender != ""
	if hasSender {
		col := p.Senders[hashIndex(m0.SenderID+m0.Sender, len(p.Senders))]
		sgtx := cgtx
		sgtx.Constraints.Max.X = gridW - 2*textInset
		sender = record(sgtx, u.label(13, m0.Sender, col, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout)
	}
	if m0.Forwarded {
		fwd = record(cgtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(iconW(icForward, 16, secondary)),
				layout.Rigid(layout.Spacer{Width: 4}.Layout),
				layout.Rigid(u.label(13, "Forwarded", secondary, labelOpts{italic: true, maxLines: 1}).Layout),
			)
		})
	}
	if m0.Quote != nil {
		quote = record(cgtx, func(gtx C) D {
			return u.layoutQuote(gtx, m0.Quote, quoteBg, secondary, gridW, u.quotedMessage(m0.Quote))
		})
	}

	// Place everything, then paint the bubble behind it.
	macro := op.Record(gtx.Ops)
	y := 0
	if hasSender {
		y += gtx.Dp(3)
		sender.at(gtx, textInset, y)
		u.senderButton(gtx, m0, image.Pt(textInset, y), sender.size)
		y += sender.size.Y + gtx.Dp(5)
	}
	if m0.Forwarded {
		y += gtx.Dp(2)
		fwd.at(gtx, textInset, y)
		y += fwd.size.Y + gtx.Dp(3)
	}
	if m0.Quote != nil {
		quote.at(gtx, 0, y)
		t := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
		qg := gtx
		qg.Constraints = layout.Exact(quote.size)
		clickable(qg, u.btn("quote:"+m0.ID), func(gtx C) D { return D{Size: quote.size} })
		t.Pop()
		y += quote.size.Y + gtx.Dp(5)
	}
	maxPx := gtx.Dp(330) // as a picture on its own, so they share the decoded copy
	for i, tr := range tiles {
		m := ms[i]
		tr = tr.Add(image.Pt(0, y))
		tiles[i] = tr.Add(image.Pt(pad, pad))
		u.layoutImage(gtx, tr, m, u.messageImage(m, maxPx))
		if i == 3 && len(ms) > 4 {
			// The last tile is covered by how many it stands for.
			more := len(ms) - 3
			fillRRect(gtx, tr, gtx.Dp(6), argb(0x000000, 0x80))
			l := record(gtx, u.label(28, "+"+strconv.Itoa(more), white).Layout)
			l.at(gtx, tr.Min.X+(tr.Dx()-l.size.X)/2, tr.Min.Y+(tr.Dy()-l.size.Y)/2)
		}
		t := op.Offset(tr.Min).Push(gtx.Ops)
		ig := gtx
		ig.Constraints = layout.Exact(tr.Size())
		clickable(ig, u.btn("img:"+m.ID), func(gtx C) D { return D{Size: gtx.Constraints.Max} })
		t.Pop()
	}
	y = tiles[len(tiles)-1].Max.Y - pad
	meta.at(gtx, gridW-meta.size.X-gtx.Dp(7), y-meta.size.Y-gtx.Dp(5))
	content := macro.Stop()

	w, h := gridW+2*pad, y+2*pad
	u.paintBubble(gtx, w, h, bg, out, r.first)
	t := op.Offset(image.Pt(pad, pad)).Push(gtx.Ops)
	content.Add(gtx.Ops)
	t.Pop()
	dims := D{Size: image.Pt(w, h)}
	if react := albumReactions(ms); react != "" {
		dims = u.reactionPill(gtx, dims, react, out)
	}
	return dims, tiles
}

// reactionPill draws the reactions to an album at the bottom of its
// bubble, as layoutMessage does for a message, and returns the row's size.
func (u *UI) reactionPill(gtx C, dims D, react string, out bool) D {
	p := u.pal
	pill := record(gtx, func(gtx C) D {
		gtx.Constraints.Min = image.Point{}
		return u.card(gtx, 13, p.BubbleIn, func(gtx C) D {
			return layout.Inset{Left: 6, Right: 6, Top: 2, Bottom: 3}.Layout(gtx, u.label(14, react, p.Text).Layout)
		})
	})
	x := gtx.Dp(8)
	if out {
		x = dims.Size.X - pill.size.X - gtx.Dp(8)
	}
	ring := gtx.Dp(2)
	py := dims.Size.Y - gtx.Dp(5)
	fillRRect(gtx, image.Rect(x-ring, py-ring, x+pill.size.X+ring, py+pill.size.Y+ring), pill.size.Y/2+ring, p.ChatBg)
	pill.at(gtx, x, py)
	dims.Size.Y = py + pill.size.Y + ring
	return dims
}
