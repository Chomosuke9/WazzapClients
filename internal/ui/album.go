package ui

import (
	"image"
	"slices"
	"strconv"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// An album is photos and videos sent together (model.Message.Album). They
// show as one bubble, a grid of up to four tiles; from five on, the fourth
// is covered by "+N" for it and the rest, and the viewer pages through all
// of them. Their captions (usually one picture's, the first) go under the
// grid. Each picture keeps its own menu and selection, from its tile or
// its caption; a reply from there is to the whole album, unless the menu's
// "Reply to this photo" is picked.

// albumMember reports whether m shows in its album's grid.
func albumMember(m *model.Message) bool {
	return m.Album != "" && m.Kind == model.KindImage
}

// joinsAlbum reports whether m goes in the grid first starts. A picture
// that replies to something else starts one of its own, and so does one
// kept after it was deleted, which says who deleted it.
func joinsAlbum(first, m *model.Message) bool {
	return albumMember(first) && albumMember(m) && m.Album == first.Album &&
		(m.Quote == nil || first.Quote != nil && m.Quote.ID == first.Quote.ID && m.Quote.ID != "") &&
		m.Revoked.IsZero() == first.Revoked.IsZero() && m.DeletedBy == first.DeletedBy && m.DeletedByMe == first.DeletedByMe &&
		m.FromMe == first.FromMe && m.SenderID == first.SenderID && m.Sender == first.Sender
}

// albumCaption is where a caption of an album is in its bubble.
type albumCaption struct {
	msg *model.Message
	r   image.Rectangle
}

// albumFile reports whether a picked file goes in an album with the
// others sent with it: photos and videos, unless they're view once or Zaps.
func albumFile(f *attachFile) bool {
	return (f.Media == model.MediaImage || f.Media == model.MediaVideo) && !f.ViewOnce && !f.Zap
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

// wholeAlbum is the message to reply to for a reply to all of ms, the
// pictures of an album.
func wholeAlbum(ms []*model.Message) *model.Message {
	whole := *ms[0]
	n := &model.AlbumSize{}
	for _, m := range ms {
		if m.Media == model.MediaVideo {
			n.Videos++
		} else {
			n.Photos++
		}
	}
	whole.Whole = n
	return &whole
}

// albumOf returns the pictures of the open chat's grid that m is in, or
// nil.
func (u *UI) albumOf(c *model.Chat, m *model.Message) []*model.Message {
	if c == nil || !albumMember(m) {
		return nil
	}
	for _, r := range u.rows(c) {
		if r.album && r.has(m.ID) {
			return r.group
		}
	}
	return nil
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

// layoutAlbumRow draws an album's row: its bubble, the sender's avatar in
// groups and a chevron on the hovered tile. In select mode, clicking the
// row picks all its pictures.
func (u *UI) layoutAlbumRow(gtx C, c *model.Chat, r convRow, maxW, margin int) D {
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
	cgtx := gtx
	cgtx.Constraints = layout.Constraints{Max: image.Pt(w, gtx.Constraints.Max.Y)}
	var tiles []image.Rectangle // in the bubble
	var captions []albumCaption
	// Privacy mode shows the album under the pointer, or being used.
	hoverKey := "album:" + m0.ID
	reveal := u.hovered[hoverKey] || slices.ContainsFunc(ms, func(m *model.Message) bool {
		return u.ctx.isOpen() && u.ctx.msg == m || u.textSel.id == m.ID
	})
	unhide := u.hiding(gtx, "msg:"+m0.ID, reveal)
	bubble := record(cgtx, func(gtx C) D {
		var dims D
		dims, tiles, captions = u.layoutAlbum(gtx, c, r, maxW)
		return dims
	})
	x := 0
	if out {
		x = w - bubble.size.X
	}
	h := bubble.size.Y
	band := image.Rect(-margin, -gtx.Dp(2), w+margin, h+gtx.Dp(2))
	if u.conv.flash != "" && r.has(u.conv.flash) {
		u.drawFlash(gtx, band)
	}
	if sel && u.pickedAny(ms) {
		fillRect(gtx, u.selBand(margin, w, h), u.selColor())
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
	unhide()
	if !sel {
		// A double click on the album or beside it replies to all of it.
		// On a caption's words it selects one instead.
		_, _, double := u.pressArea(gtx, hoverKey, image.Rect(-margin, 0, w+margin, h))
		words := u.textSel.clicks >= 2 && slices.ContainsFunc(ms, func(m *model.Message) bool { return m.ID == u.textSel.id })
		if double && !words && u.sendBlocked(c) == "" {
			u.startReply(wholeAlbum(ms))
		}
		for i, tr := range tiles {
			u.albumTile(gtx, ms[i], tr.Add(image.Pt(x, 0)))
		}
		for _, cp := range captions {
			u.albumCaptionArea(gtx, cp.msg, cp.r.Add(image.Pt(x, 0)))
		}
		// On top of the tiles and their chevrons, letting events through.
		t := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		u.hoverArea(gtx, hoverKey, bubble.size)
		t.Pop()
	}
	if sel {
		t := op.Offset(image.Pt(-margin, -u.conv.band[0])).Push(gtx.Ops)
		rg := gtx
		rg.Constraints = layout.Exact(u.selBand(margin, w, h).Size())
		clickable(rg, u.btn(rowKey), func(gtx C) D { return D{Size: gtx.Constraints.Max} })
		t.Pop()
	}
	return D{Size: image.Pt(w, h)}
}

// albumTile handles one tile of an album, at tr: right click opens its
// picture's menu, and hovering shows its chevron.
func (u *UI) albumTile(gtx C, m *model.Message, tr image.Rectangle) {
	defer op.Offset(tr.Min).Push(gtx.Ops).Pop()
	sz := tr.Size()
	hovered := u.hoverArea(gtx, m.ID, sz)
	if u.rightClick(gtx, m.ID, sz) {
		u.openMessageMenu(m)
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

// albumCaptionArea handles the caption of m, at r: right click opens its
// picture's menu.
func (u *UI) albumCaptionArea(gtx C, m *model.Message, r image.Rectangle) {
	if right, _, _ := u.pressArea(gtx, "caption:"+m.ID, r); right {
		u.openMessageMenu(m)
	}
}

// layoutAlbum draws an album's bubble, sized to its grid, and returns
// where its tiles and captions are in it.
func (u *UI) layoutAlbum(gtx C, c *model.Chat, r convRow, maxW int) (D, []image.Rectangle, []albumCaption) {
	p := u.pal
	ms := r.group
	m0 := ms[0]
	out := m0.FromMe
	bg, quoteBg, textCol, metaCol, secondary := p.BubbleIn, p.QuoteIn, p.Text, p.MetaIn, p.TextSecondary
	if out {
		bg, quoteBg, textCol, metaCol, secondary = p.BubbleOut, p.QuoteOut, p.TextOut, p.MetaOut, p.SecondaryOut
	}
	var captioned []*model.Message
	for _, m := range ms {
		if m.Text != "" {
			captioned = append(captioned, m)
		}
	}
	pad := gtx.Dp(3)
	gridW := min(maxW-2*pad, gtx.Dp(330))
	textInset := gtx.Dp(6)
	cgtx := gtx
	cgtx.Constraints = layout.Constraints{Max: image.Pt(gridW, 1<<20)}
	tiles := albumTiles(len(ms), gridW, gtx.Dp(3))
	for i := range tiles {
		if cl := u.btn("img:" + ms[i].ID); cl.Clicked(gtx) {
			u.openViewer(ms[i])
			u.viewer.origin = u.viewerOrigin(gtx, cl, tiles[i].Size())
		}
	}
	u.quoteClicked(gtx, m0)

	white := rgb(0xffffff)
	// The time is on the last tile, or after the last caption.
	meta := record(cgtx, func(gtx C) D {
		if len(captioned) > 0 {
			return u.layoutMeta(gtx, albumMeta(ms), metaCol, nil)
		}
		return u.layoutMeta(gtx, albumMeta(ms), white, &white)
	})
	var sender, fwd, quote part
	hasSender := c.IsGroup && !out && r.first && m0.Sender != ""
	if hasSender {
		sgtx := cgtx
		sgtx.Constraints.Max.X = gridW - 2*textInset
		sender = record(sgtx, func(gtx C) D { return u.layoutSenderName(gtx, m0, p.BubbleIn) })
	}
	if m0.Forwarded {
		fwd = record(cgtx, func(gtx C) D {
			return u.forwardedLabel(gtx, m0, secondary)
		})
	}
	if m0.Quote != nil {
		quote = record(cgtx, func(gtx C) D {
			return u.layoutQuote(gtx, m0.ChatID, m0.Quote, quoteBg, secondary, gridW, u.quotedMessage(m0.Quote))
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
		img := u.gifImage(m, u.messageImage(m, maxPx), maxPx)
		covered := i == 3 && len(ms) > 4
		var pill part
		if covered {
			u.layoutImage(gtx, tr, m, img)
		} else {
			var metaSz image.Point
			if i == len(tiles)-1 && len(captioned) == 0 {
				metaSz = meta.size.Add(image.Pt(gtx.Dp(7), 0))
			}
			pill = u.layoutPicture(gtx, tr, m, img, metaSz)
		}
		if covered {
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
		pill.at(gtx, 0, 0)
	}
	y = tiles[len(tiles)-1].Max.Y - pad
	var captions []albumCaption
	if len(captioned) == 0 {
		meta.at(gtx, gridW-meta.size.X-gtx.Dp(7), y-meta.size.Y-gtx.Dp(5))
	}
	for i, m := range captioned {
		const textSize = unit.Sp(15.7)
		y += gtx.Dp(5)
		var o richOpts
		if i == len(captioned)-1 {
			o.suffix = u.metaSpacer(gtx, textSize, meta.size.X)
		}
		text := u.textOpts(gtx, c, m, m.Text, &o)
		tgtx := cgtx
		tgtx.Constraints.Max.X = gridW - 2*textInset
		body := record(tgtx, func(gtx C) D { return u.layoutRich(gtx, text, textSize, textCol, secondary, o) })
		body.at(gtx, textInset, y)
		captions = append(captions, albumCaption{m, image.Rect(0, y, gridW, y+body.size.Y).Add(image.Pt(pad, pad))})
		y += body.size.Y
	}
	if len(captioned) > 0 {
		meta.at(gtx, gridW-meta.size.X-textInset, y-meta.size.Y+gtx.Dp(4))
		y += gtx.Dp(5)
	}
	content := macro.Stop()

	w, h := gridW+2*pad, y+2*pad
	u.paintBubble(gtx, w, h, bg, out, r.first)
	t := op.Offset(image.Pt(pad, pad)).Push(gtx.Ops)
	content.Add(gtx.Ops)
	t.Pop()
	dims := D{Size: image.Pt(w, h)}
	bubble := dims.Size
	dims = u.reactionPill(gtx, dims, ms, out, false, part{}, animKey{})
	return u.stickerReactions(gtx, ms, bubble, dims, out), tiles, captions
}
