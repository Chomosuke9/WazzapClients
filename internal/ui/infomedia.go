package ui

import (
	"image"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// A chat's "Media, links and docs", a page of the info panel like WhatsApp
// Web's: the Media panel's tabs, tiles and rows (gallery.go) for one chat.

// openInfoGallery starts the page on the chat's media.
func (u *UI) openInfoGallery() {
	u.info.gallery = galleryState{chatID: u.info.chatID, keys: "info:"}
	u.loadInfoGallery()
}

// loadInfoGallery asks for the first page of the open tab.
func (u *UI) loadInfoGallery() {
	g := &u.info.gallery
	u.info.list.Position = layout.Position{}
	g.list.start(u.backend, model.GalleryQuery{Kind: g.tab, ChatID: g.chatID, Limit: galleryPage})
}

// mediaRow is a row of the page: a month's heading, or the messages from
// index from up to to, side by side.
type mediaRow struct {
	heading  string
	from, to int
}

// mediaRows splits a tab's messages into rows; media go cols to a row, under
// their months.
func mediaRows(msgs []*model.Message, tab model.GalleryKind, cols int, now time.Time) []mediaRow {
	if tab != model.GalleryMedia {
		cols = 1
	}
	var rows []mediaRow
	month := ""
	for i := range msgs {
		if tab == model.GalleryMedia {
			if h := mediaMonth(msgs[i].Time, now); h != month {
				month = h
				rows = append(rows, mediaRow{heading: h})
			}
		}
		if n := len(rows) - 1; n >= 0 && rows[n].heading == "" && rows[n].to-rows[n].from < cols {
			rows[n].to = i + 1
			continue
		}
		rows = append(rows, mediaRow{from: i, to: i + 1})
	}
	return rows
}

// mediaMonth is the heading of the month t is in.
func mediaMonth(t, now time.Time) string {
	t = t.In(now.Location())
	switch {
	case t.Year() != now.Year():
		return strings.ToUpper(t.Format("January 2006"))
	case t.Month() != now.Month():
		return strings.ToUpper(t.Month().String())
	}
	return "THIS MONTH"
}

// layoutInfoGallery draws the page under the panel's header.
func (u *UI) layoutInfoGallery(gtx C) D {
	g := &u.info.gallery
	for _, t := range galleryTabs {
		if u.btn("info:gal:tab:"+itoa(int(t.kind))).Clicked(gtx) && g.tab != t.kind {
			g.tab = t.kind
			u.loadInfoGallery()
		}
	}
	if u.btn("info:gal:all").Clicked(gtx) {
		u.openGallery("", "")
		if u.gallery.tab != g.tab {
			u.gallery.tab = g.tab
			u.loadGallery()
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(u.infoGalleryTabs),
		layout.Flexed(1, u.infoGalleryBody),
		layout.Rigid(u.infoGalleryFooter),
	)
}

func (u *UI) infoGalleryTabs(gtx C) D {
	g := &u.info.gallery
	p := u.pal
	sz := image.Pt(gtx.Constraints.Max.X, gtx.Dp(48))
	fillRect(gtx, image.Rect(0, sz.Y-max(1, gtx.Dp(1)), sz.X, sz.Y), p.Divider)
	tw := sz.X / len(galleryTabs)
	cur := 0
	for i, t := range galleryTabs {
		col := p.TextSecondary
		if g.tab == t.kind {
			cur, col = i, p.Text
		}
		c := u.btn("info:gal:tab:" + itoa(int(t.kind)))
		tr := op.Offset(image.Pt(i*tw, 0)).Push(gtx.Ops)
		tg := gtx
		tg.Constraints = layout.Exact(image.Pt(tw, sz.Y))
		clickable(tg, c, func(gtx C) D {
			if a := u.hover(gtx, c); a > 0 {
				fillRect(gtx, image.Rectangle{Max: gtx.Constraints.Max}, faded(p.Hover, a*0.6))
			}
			return layout.Center.Layout(gtx, u.label(15.5, t.label, col, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
		})
		tr.Pop()
	}
	x := int(g.tabX.step(gtx, float32(cur*tw), durSwitch))
	fillRect(gtx, image.Rect(x, sz.Y-gtx.Dp(3), x+tw, sz.Y), p.Green)
	return D{Size: sz}
}

// infoGalleryBody lists the open tab's messages.
func (u *UI) infoGalleryBody(gtx C) D {
	if !u.gallery.open {
		defer u.mediaGallery.track(gtx, u) // the Media panel, when it's open, tracks its own
	}
	g := &u.info.gallery
	l := &g.list
	p := u.pal
	sz := gtx.Constraints.Max
	gtx.Constraints.Min = sz
	if len(l.msgs) == 0 {
		txt := map[model.GalleryKind]string{model.GalleryMedia: "No media", model.GalleryDocs: "No documents",
			model.GalleryLinks: "No links"}[g.tab]
		if l.loading {
			txt = "Loading…"
		}
		return layout.Center.Layout(gtx, u.label(16, txt, p.TextSecondary).Layout)
	}
	const padX = unit.Dp(infoPadX)
	gap := gtx.Dp(6)
	w := sz.X - 2*gtx.Dp(padX)
	cols := max(3, w/gtx.Dp(140))
	tile := (w - (cols-1)*gap) / cols
	rows := mediaRows(l.msgs, g.tab, cols, u.now())
	return u.scrollList(gtx, &u.info.list, len(rows), func(gtx C, i int) D {
		if i == len(rows)-1 {
			l.next(u.backend) // the last row is in view: load the next page
		}
		r := rows[i]
		switch {
		case r.heading != "":
			return layout.Inset{Left: padX, Right: padX, Top: 16, Bottom: 8}.Layout(gtx,
				u.label(13.5, r.heading, p.Text, labelOpts{maxLines: 1}).Layout)
		case g.tab != model.GalleryMedia:
			return u.galleryRow(gtx, g, l.msgs[r.from])
		}
		for k := r.from; k < r.to; k++ {
			tr := op.Offset(image.Pt(gtx.Dp(padX)+(k-r.from)*(tile+gap), 0)).Push(gtx.Ops)
			tg := gtx
			tg.Constraints = layout.Exact(image.Pt(tile, tile))
			u.galleryTile(tg, g, l.msgs[k])
			tr.Pop()
		}
		return D{Size: image.Pt(gtx.Constraints.Max.X, tile+gap)}
	})
}

// infoGalleryFooter leads to the Media panel.
func (u *UI) infoGalleryFooter(gtx C) D {
	p := u.pal
	c := u.btn("info:gal:all")
	gtx.Constraints = layout.Exact(image.Pt(gtx.Constraints.Max.X, gtx.Dp(56)))
	return clickable(gtx, c, func(gtx C) D {
		sz := gtx.Constraints.Max
		if a := u.hover(gtx, c); a > 0 {
			fillRect(gtx, image.Rectangle{Max: sz}, faded(p.RowHover, a))
		}
		fillRect(gtx, image.Rect(0, 0, sz.X, max(1, gtx.Dp(1))), p.Divider)
		return layout.Center.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(iconW(icPhotoLibrary, 22, p.Green)),
				layout.Rigid(layout.Spacer{Width: 10}.Layout),
				layout.Rigid(u.label(15, "View media from all chats", p.Green, labelOpts{weight: font.Medium, maxLines: 1}).Layout),
			)
		})
	})
}
