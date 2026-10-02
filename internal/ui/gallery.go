package ui

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// The Media panel: photos and videos, documents and links from every
// chat (the rail's Media button) or from one ("Media, links and docs" in
// its info panel), like WhatsApp Desktop's. It slides up over the window;
// pages of 60 load as it scrolls.

// galleryPage is how many messages a page of the panel loads.
const galleryPage = 60

type galleryState struct {
	open   bool
	anim   tween
	chatID string // "" for every chat
	name   string // the chat's name, for the subtitle
	tab    model.GalleryKind
	tabX   follower // the tab underline sliding between tabs
	oldest bool

	searching bool
	query     widget.Editor
	ran       string // the query the list shows

	list   galleryList
	scroll widget.List

	selecting bool
	picked    []*model.Message
}

// galleryTabs are the panel's tabs.
var galleryTabs = []struct {
	kind  model.GalleryKind
	label string
}{{model.GalleryMedia, "Media"}, {model.GalleryDocs, "Docs"}, {model.GalleryLinks, "Links"}}

// openGallery opens the panel on a chat's media, or every chat's when
// chatID is "".
func (u *UI) openGallery(chatID, name string) {
	g := &u.gallery
	if !g.open || g.chatID != chatID {
		*g = galleryState{chatID: chatID, name: name, anim: g.anim}
	}
	g.open = true
	g.query.SingleLine = true
	g.scroll.Axis = layout.Vertical
	u.ctx = ctxMenu{}
	u.loadGallery()
}

func (u *UI) closeGallery() {
	g := &u.gallery
	g.open, g.selecting, g.picked, g.searching = false, false, nil, false
}

// loadGallery asks for the first page of the panel's tab.
func (u *UI) loadGallery() {
	g := &u.gallery
	g.ran = g.query.Text()
	g.scroll.Position = layout.Position{}
	q := model.GalleryQuery{Kind: g.tab, ChatID: g.chatID, Oldest: g.oldest, Limit: galleryPage}
	if g.searching {
		q.Text = g.ran
	}
	g.list.start(u.backend, q)
}

// galleryKey names a message in the panel's buttons and selection.
func galleryKey(m *model.Message) string { return m.ChatID + "/" + m.ID }

func (g *galleryState) pickedAt(m *model.Message) int {
	for i, x := range g.picked {
		if x.ChatID == m.ChatID && x.ID == m.ID {
			return i
		}
	}
	return -1
}

// galleryClick opens a message, or picks it in select mode.
func (u *UI) galleryClick(m *model.Message) {
	g := &u.gallery
	if g.selecting {
		if i := g.pickedAt(m); i >= 0 {
			g.picked = append(g.picked[:i:i], g.picked[i+1:]...)
		} else {
			g.picked = append(g.picked, m)
		}
		return
	}
	switch g.tab {
	case model.GalleryMedia:
		var items []*model.Message
		for _, x := range g.list.msgs {
			if x.Kind == model.KindImage || x.Media == model.MediaImage {
				items = append(items, x)
			}
		}
		if g.chatID == "" || !g.oldest {
			// The viewer goes oldest to newest, left to right.
			for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
				items[i], items[j] = items[j], items[i]
			}
		}
		u.openViewerOn(items, m)
	case model.GalleryDocs:
		u.openDocument(m)
	case model.GalleryLinks:
		if l := firstLink(m.Text); l != "" {
			u.openLink(l)
		}
	}
}

// firstLink returns the first web link in s.
func firstLink(s string) string {
	loc := linkRe.FindStringIndex(s)
	if loc == nil {
		return ""
	}
	return trimLink(s[loc[0]:loc[1]])
}

// galleryUpdate handles the panel's buttons before it's drawn.
func (u *UI) galleryUpdate(gtx C) {
	g := &u.gallery
	if u.rail.media.Clicked(gtx) {
		if g.open && g.chatID == "" {
			u.closeGallery()
		} else {
			u.openGallery("", "")
		}
	}
	if !g.open {
		return
	}
	if u.btn("gal:close").Clicked(gtx) {
		u.closeGallery()
	}
	if u.btn("gal:scrim").Clicked(gtx) {
		u.closeGallery()
	}
	for _, t := range galleryTabs {
		if u.btn("gal:tab:"+itoa(int(t.kind))).Clicked(gtx) && g.tab != t.kind {
			g.tab = t.kind
			g.picked = nil
			u.loadGallery()
		}
	}
	if u.btn("gal:search").Clicked(gtx) {
		g.searching = !g.searching
		if g.searching {
			u.requestFocus(&g.query)
		} else {
			g.query.SetText("")
		}
		u.loadGallery()
	}
	if g.searching && g.query.Text() != g.ran {
		u.loadGallery()
	}
	if u.btn("gal:sort").Clicked(gtx) {
		u.ctx = ctxMenu{kind: ctxGallerySort, at: u.mouse}
	}
	if u.btn("gal:select").Clicked(gtx) {
		g.selecting = !g.selecting
		g.picked = nil
	}
	if len(g.picked) > 0 {
		picked := append([]*model.Message(nil), g.picked...)
		if u.btn("gal:forward").Clicked(gtx) {
			u.openForward(picked)
			g.selecting, g.picked = false, nil
		}
		if u.btn("gal:save").Clicked(gtx) {
			for _, m := range picked {
				u.backend.SaveMedia(m)
			}
			g.selecting, g.picked = false, nil
		}
		if u.btn("gal:delete").Clicked(gtx) {
			u.confirmDelete(picked)
			g.selecting, g.picked = false, nil
		}
	}
}

// gallerySortItems is the sort button's menu.
func (u *UI) gallerySortItems() []menuItem {
	g := &u.gallery
	set := func(oldest bool) func() {
		return func() {
			if g.oldest != oldest {
				g.oldest = oldest
				u.loadGallery()
			}
		}
	}
	return []menuItem{
		{key: "newest", label: "Newest first", tick: !g.oldest, run: set(false)},
		{key: "oldest", label: "Oldest first", tick: g.oldest, run: set(true)},
	}
}

// layoutGallery draws the panel over the window.
func (u *UI) layoutGallery(gtx C) {
	g := &u.gallery
	v := g.anim.step(gtx, g.open, durDialog)
	if v == 0 && !g.open {
		if g.list.msgs != nil {
			// Closed: let its pictures go.
			for _, m := range g.list.msgs {
				u.images.forget("m:" + m.ChatID + "/" + m.ID)
				u.images.forget("t:" + m.ChatID + "/" + m.ID)
			}
			g.list = galleryList{}
		}
		return
	}
	p := u.pal
	e := easeOut(v)
	sz := gtx.Constraints.Max
	if !g.open {
		var done func()
		gtx, done = fadeOut(gtx)
		defer done()
		fillRect(gtx, image.Rectangle{Max: sz}, faded(p.Scrim, e))
	} else {
		sg := gtx
		sg.Constraints = layout.Exact(sz)
		clickable(sg, u.btn("gal:scrim"), func(gtx C) D { return fill(gtx, faded(p.Scrim, e)) })
	}
	// The panel slides up; a fade would need a window-sized layer.
	m := gtx.Dp(12)
	r := image.Rect(m, m, sz.X-m, sz.Y-m)
	dy := int(float32(sz.Y) * (1 - e))
	defer op.Offset(image.Pt(r.Min.X, r.Min.Y+dy)).Push(gtx.Ops).Pop()
	pg := gtx
	pg.Constraints = layout.Exact(r.Size())
	fillRRect(pg, image.Rectangle{Max: r.Size()}.Add(image.Pt(0, gtx.Dp(4))).Inset(-gtx.Dp(3)), gtx.Dp(17), p.Shadow)
	fillRRect(pg, image.Rectangle{Max: r.Size()}, gtx.Dp(14), p.Panel)
	// Swallow clicks on the panel so they don't reach the scrim.
	u.btn("gal:panel").Layout(pg, func(gtx C) D { return D{Size: gtx.Constraints.Max} })
	layout.Flex{Axis: layout.Vertical}.Layout(pg,
		layout.Rigid(u.galleryHeader),
		layout.Flexed(1, func(gtx C) D {
			// The list stops above the rounded corners, so nothing needs
			// clipping to them.
			return layout.Inset{Bottom: 14}.Layout(gtx, u.galleryBody)
		}),
	)
}

// galleryHeader is the title, the tabs (or the search field) and the
// buttons, or the selection's actions in select mode.
func (u *UI) galleryHeader(gtx C) D {
	g := &u.gallery
	p := u.pal
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	h := gtx.Dp(72)
	w := gtx.Constraints.Max.X
	title, sub := "Media", "Media from all chats"
	if g.chatID != "" {
		title, sub = "Media, links and docs", g.name
	}
	if g.selecting {
		title, sub = "Select items", "Click items to select them"
		if n := len(g.picked); n > 0 {
			title = itoa(n) + " selected"
		}
	}
	tl := record(gtx, func(gtx C) D {
		gtx.Constraints.Max.X = min(gtx.Dp(260), w/4)
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(u.label(17, title, p.Text, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout),
			layout.Rigid(layout.Spacer{Height: 2}.Layout),
			layout.Rigid(u.label(14.5, sub, p.TextSecondary, labelOpts{maxLines: 1}).Layout),
		)
	})
	tl.at(gtx, gtx.Dp(30), (h-tl.size.Y)/2)

	// The buttons, from the right.
	type btn struct {
		key string
		ic  *icon.Icon
		on  bool
	}
	btns := []btn{{"close", icClose, false}, {"select", icCheckBox, g.selecting}, {"sort", icSort, false}, {"search", icSearch, g.searching}}
	if len(g.picked) > 0 {
		btns = []btn{{"close", icClose, false}, {"select", icCheckBox, true}, {"delete", icDelete, false},
			{"save", icDownload, false}, {"forward", icForward, false}}
	}
	x := w - gtx.Dp(20)
	for _, b := range btns {
		x -= gtx.Dp(44)
		col := p.IconStrong
		if b.on {
			col = p.Green
		}
		t := op.Offset(image.Pt(x, (h-gtx.Dp(40))/2)).Push(gtx.Ops)
		u.iconButton(gtx, u.btn("gal:"+b.key), b.ic, 40, 25, col)
		t.Pop()
		x -= gtx.Dp(8)
	}

	// The tabs (or the search field) in the middle.
	tw := gtx.Dp(136)
	left := (w - tw*len(galleryTabs)) / 2
	left = max(left, gtx.Dp(30)+tl.size.X+gtx.Dp(16))
	if g.searching {
		sw := min(gtx.Dp(420), x-left-gtx.Dp(16))
		t := op.Offset(image.Pt(left, (h-gtx.Dp(40))/2)).Push(gtx.Ops)
		sg := gtx
		sg.Constraints = layout.Exact(image.Pt(max(0, sw), gtx.Dp(40)))
		u.searchField(sg, &g.query, "Search")
		t.Pop()
	} else {
		cur := 0
		for i, t := range galleryTabs {
			c := u.btn("gal:tab:" + itoa(int(t.kind)))
			on := g.tab == t.kind
			if on {
				cur = i
			}
			col := p.TextSecondary
			if on {
				col = p.Text
			}
			func() {
				defer op.Offset(image.Pt(left+i*tw, 0)).Push(gtx.Ops).Pop()
				tg := gtx
				tg.Constraints = layout.Exact(image.Pt(tw, h))
				clickable(tg, c, func(gtx C) D {
					if a := u.hover(gtx, c); a > 0 {
						fillRect(gtx, image.Rectangle{Max: gtx.Constraints.Max}, faded(p.Hover, a*0.6))
					}
					return layout.Center.Layout(gtx, u.label(17, t.label, col, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
				})
			}()
		}
		ux := g.tabX.step(gtx, float32(left+cur*tw), durSwitch)
		fillRect(gtx, image.Rect(int(ux), h-gtx.Dp(3), int(ux)+tw, h), p.Green)
	}
	fillRect(gtx, image.Rect(0, h-1, w, h), p.Divider)
	return D{Size: image.Pt(w, h)}
}

// galleryBody lists the tab's messages: a grid of pictures, or rows of
// documents or links.
func (u *UI) galleryBody(gtx C) D {
	g := &u.gallery
	p := u.pal
	l := &g.list
	sz := gtx.Constraints.Max
	if len(l.msgs) == 0 {
		txt := map[model.GalleryKind]string{model.GalleryMedia: "No media", model.GalleryDocs: "No documents",
			model.GalleryLinks: "No links"}[g.tab]
		if l.loading {
			txt = "Loading…"
		} else if g.searching && g.ran != "" {
			txt = "No results found"
		}
		gtx.Constraints.Min = sz
		return layout.Center.Layout(gtx, u.label(16, txt, p.TextSecondary).Layout)
	}
	cols := 1
	if g.tab == model.GalleryMedia {
		cols = max(2, (sz.X+gtx.Dp(120))/gtx.Dp(240))
	}
	n := (len(l.msgs) + cols - 1) / cols
	gtx.Constraints.Min = sz
	return u.scrollList(gtx, &g.scroll, n, func(gtx C, i int) D {
		if i == n-1 {
			l.next(u.backend) // the last row is in view: load the next page
		}
		if g.tab != model.GalleryMedia {
			return u.galleryRow(gtx, l.msgs[i])
		}
		gap := gtx.Dp(3)
		w := gtx.Constraints.Max.X
		tile := (w - (cols-1)*gap) / cols
		for c := range cols {
			k := i*cols + c
			if k >= len(l.msgs) {
				break
			}
			func() {
				defer op.Offset(image.Pt(c*(tile+gap), 0)).Push(gtx.Ops).Pop()
				tg := gtx
				tg.Constraints = layout.Exact(image.Pt(tile, tile))
				u.galleryTile(tg, l.msgs[k])
			}()
		}
		return D{Size: image.Pt(w, tile+gap)}
	})
}

// galleryWho names who sent a message, for the panel: you, the group
// member, or the person you chat with.
func (u *UI) galleryWho(m *model.Message) string {
	switch {
	case m.FromMe:
		return "You"
	case m.Sender != "":
		return plainText(m.Sender)
	}
	if c := u.chatByID(m.ChatID); c != nil {
		return c.Name
	}
	return ""
}

// galleryTile is one picture or video of the grid.
func (u *UI) galleryTile(gtx C, m *model.Message) D {
	g := &u.gallery
	p := u.pal
	sz := gtx.Constraints.Max
	c := u.btn("gal:m:" + galleryKey(m))
	if c.Clicked(gtx) {
		u.galleryClick(m)
	}
	return clickable(gtx, c, func(gtx C) D {
		r := image.Rectangle{Max: sz}
		img := u.messageImage(m, sz.X)
		func() {
			defer clip.Rect(r).Push(gtx.Ops).Pop()
			switch {
			case img != nil && img.state == imgReady:
				paintCover(gtx, img.op, img.size, r)
			case m.ImageA != 0 || m.ImageB != 0:
				u.gradientImage(gtx, r, m.ImageA, m.ImageB)
			default:
				fillRect(gtx, r, p.Hover)
				t := op.Offset(image.Pt(sz.X/2-gtx.Dp(24), sz.Y/2-gtx.Dp(24))).Push(gtx.Ops)
				drawIcon(gtx, icImage, 48, p.TextSecondary)
				t.Pop()
			}
			// A shade at the bottom for the sender's name.
			sh := gtx.Dp(64)
			paint.LinearGradientOp{
				Stop1: f32.Pt(0, float32(sz.Y-sh)), Color1: color.NRGBA{},
				Stop2: f32.Pt(0, float32(sz.Y)), Color2: color.NRGBA{A: 0xa0},
			}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			if a := u.hover(gtx, c); a > 0 {
				fillRect(gtx, r, faded(argb(0x000000, 0x30), a))
			}
		}()
		white := rgb(0xffffff)
		gtx.Constraints.Min = image.Point{}
		name := record(gtx, func(gtx C) D {
			gtx.Constraints.Max.X = sz.X - gtx.Dp(24)
			return u.label(15.5, u.galleryWho(m), white, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout(gtx)
		})
		y := sz.Y - name.size.Y - gtx.Dp(8)
		name.at(gtx, gtx.Dp(12), y)
		if m.Media == model.MediaVideo || m.Media == model.MediaGIF {
			kind := record(gtx, func(gtx C) D {
				label := clock(m.Duration)
				if m.Media == model.MediaGIF {
					label = "GIF"
				}
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(iconW(icVideo, 20, white)),
					layout.Rigid(layout.Spacer{Width: 6}.Layout),
					layout.Rigid(u.label(13, label, white, labelOpts{maxLines: 1}).Layout),
				)
			})
			kind.at(gtx, gtx.Dp(12), y-kind.size.Y-gtx.Dp(2))
		}
		if g.selecting {
			box := icCheckBoxEmpty
			if g.pickedAt(m) >= 0 {
				box = icCheckBox
				fillRect(gtx, r, argb(0x5dbf6e, 0x40))
			}
			fillCircle(gtx, image.Pt(gtx.Dp(22), gtx.Dp(22)), gtx.Dp(14), argb(0x000000, 0x60))
			t := op.Offset(image.Pt(gtx.Dp(10), gtx.Dp(10))).Push(gtx.Ops)
			drawIcon(gtx, box, 24, white)
			t.Pop()
		}
		return D{Size: sz}
	})
}

// galleryRow is a document or a link, with who sent it and when.
func (u *UI) galleryRow(gtx C, m *model.Message) D {
	g := &u.gallery
	p := u.pal
	c := u.btn("gal:m:" + galleryKey(m))
	if c.Clicked(gtx) {
		u.galleryClick(m)
	}
	title, sub := m.FileName, docInfo(m)
	pic := func(gtx C) D {
		ext := fileExt(m)
		sz := gtx.Dp(44)
		fillRRect(gtx, image.Rect(0, 0, sz, sz), gtx.Dp(8), fileColor(ext))
		if ext == "" {
			return centerIn(gtx, sz, iconW(icDocumentFill, 24, rgb(0xffffff)))
		}
		return centerIn(gtx, sz, u.label(11.5, ext, rgb(0xffffff), labelOpts{weight: font.Bold, maxLines: 1}).Layout)
	}
	if g.tab == model.GalleryLinks {
		title = firstLink(m.Text)
		sub = strings.Join(strings.Fields(plainText(m.Text)), " ")
		pic = func(gtx C) D {
			sz := gtx.Dp(44)
			fillRRect(gtx, image.Rect(0, 0, sz, sz), gtx.Dp(8), p.Hover)
			return centerIn(gtx, sz, iconW(icLink, 24, p.TextSecondary))
		}
	}
	if title == "" {
		title = plainText(m.Text)
	}
	where := u.galleryWho(m)
	if ch := u.chatByID(m.ChatID); ch != nil && g.chatID == "" && ch.Name != where {
		where += " · " + ch.Name
	}
	return clickable(gtx, c, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		bg := mix(p.Panel, p.Hover, u.hover(gtx, c))
		if g.selecting && g.pickedAt(m) >= 0 {
			bg = mix(p.Panel, p.Green, 0.18)
		}
		return background(gtx, bg, 0, func(gtx C) D {
			return layout.Inset{Left: 30, Right: 30, Top: 12, Bottom: 12}.Layout(gtx, func(gtx C) D {
				children := []layout.FlexChild{}
				if g.selecting {
					box, col := icCheckBoxEmpty, p.TextSecondary
					if g.pickedAt(m) >= 0 {
						box, col = icCheckBox, p.Green
					}
					children = append(children, layout.Rigid(iconW(box, 24, col)), layout.Rigid(layout.Spacer{Width: 18}.Layout))
				}
				children = append(children,
					layout.Rigid(pic),
					layout.Rigid(layout.Spacer{Width: 16}.Layout),
					layout.Flexed(1, func(gtx C) D {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(u.label(16, title, p.Text, labelOpts{maxLines: 1}).Layout),
							layout.Rigid(layout.Spacer{Height: 3}.Layout),
							layout.Rigid(u.label(14, sub, p.TextSecondary, labelOpts{maxLines: 2}).Layout),
						)
					}),
					layout.Rigid(layout.Spacer{Width: 16}.Layout),
					layout.Rigid(func(gtx C) D {
						gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(260))
						return layout.Flex{Axis: layout.Vertical, Alignment: layout.End}.Layout(gtx,
							layout.Rigid(u.label(14, where, p.TextSecondary, labelOpts{maxLines: 1, align: text.End}).Layout),
							layout.Rigid(layout.Spacer{Height: 3}.Layout),
							layout.Rigid(u.label(13, listTime(m.Time, u.now()), p.TextSecondary, labelOpts{maxLines: 1}).Layout),
						)
					}),
				)
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
			})
		})
	})
}
