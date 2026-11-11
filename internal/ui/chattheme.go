package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/filepick"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Chat themes, as WhatsApp has them (WhatsApp Web's ChatTheme*.stylex and
// WAWebChatThemeDrawer, the Android app's Chat theme pages): a chat's look
// is a color scheme for its bubbles and accent, and a wallpaper. Picking
// one of the Themes sets both (the wallpaper follows the scheme); Chat
// bubble changes the bubbles alone and Wallpaper the wallpaper alone: a
// theme's, a plain color (with or without doodles) or a photo of yours.
// Every chat takes the look set for all chats (Settings > Appearance > Chat
// theme) until it gets one of its own. The tables are in chatthemes.go.

type schemeKind uint8

const (
	vibrant schemeKind = iota // your bubbles in the theme's color, their text white
	tonal                     // pale bubbles, dark text
	minimal                   // vibrant on a plain wallpaper without doodles
)

type chatTheme struct {
	id    string // WhatsApp's color scheme ID ("Pink@Tonal"); "" is Default
	base  string // the theme's ID ("Pink")
	kind  schemeKind
	stock bool // shown with a photo wallpaper in WhatsApp: the second section
	light themeColors
	dark  themeColors
}

// themeColors are a scheme's colors in one mode, as 0xRRGGBB.
type themeColors struct {
	wall, doodle      uint32 // doodle 0: none
	out, in           uint32 // bubbles
	metaIn, quoteIn   uint32
	quoteOut, metaOut uint32
	textOut, linkOut  uint32 // white in a vibrant scheme's bubbles
	readOut           uint32 // read ticks in your bubbles
	accent, link      uint32 // the chat's accent, links in their bubbles
	read              uint32
}

// solidWall is a plain color wallpaper.
type solidWall struct {
	light, dark uint32
	doodle      float32 // the doodles' opacity on the light color
	name        string
}

// prefChatTheme keys a chat's look in Backend.Pref: + the chat's ID, or
// lookAll for every chat's. Its value is the scheme's ID, then "|" and the
// wallpaper when that doesn't follow the scheme (wallFollow): "default",
// "theme:<theme ID>", "color:<index in solidWalls>" or "photo:<file in
// wallpaperDir>". A chat without a value takes lookAll's; "|" is a chat
// set back to the app's own look.
const (
	prefChatTheme = "theme:"
	lookAll       = "*"
)

// chatLook is a chat's scheme (an index in chatThemes) and wallpaper.
type chatLook struct {
	scheme int
	wall   string // "" follows the scheme
}

// oldChatThemes are the themes kept before WhatsApp's and the ones they
// became. Pink and Red kept their names.
var oldChatThemes = map[string]string{
	"Teal": "Emerald", "Blue": "Default-Blue", "Purple": "Pearl-Indigo",
	"Orange": "Sunrise-Orange", "Yellow": "Warm-Yellow", "Slate": "Default-Mono",
}

// wallpaperDir keeps the photos chosen as wallpapers (Options.WallpaperDir).
var wallpaperDir string

// schemeIndex returns the index of the scheme with WhatsApp's ID id.
func schemeIndex(id string) int {
	if n, ok := oldChatThemes[id]; ok {
		id = n
	}
	for i, t := range chatThemes {
		if id != "" && t.id == id {
			return i
		}
	}
	return 0
}

// lookOf returns chat id's look (id may be lookAll).
func (u *UI) lookOf(id string) chatLook {
	if u.themes == nil {
		u.themes = map[string]chatLook{}
	}
	if l, ok := u.themes[id]; ok {
		return l
	}
	v := u.backend.Pref(prefChatTheme + id)
	var l chatLook
	if v == "" && id != lookAll {
		l = u.lookOf(lookAll)
	} else {
		scheme, wall, _ := strings.Cut(v, "|")
		l = chatLook{schemeIndex(scheme), wall}
	}
	u.themes[id] = l
	return l
}

// setLook keeps chat id's look.
func (u *UI) setLook(id string, l chatLook) {
	v := chatThemes[l.scheme].id
	if l.wall != "" {
		v += "|" + l.wall
	}
	if v == "" && id != lookAll && u.backend.Pref(prefChatTheme+lookAll) != "" {
		v = "|" // the app's look, not every chat's
	}
	u.backend.SetPref(prefChatTheme+id, v)
	u.themes = nil // lookAll's changes every chat without one
	u.lookPal = lookPal{}
}

// chatThemeOf returns the index of a chat's scheme in chatThemes.
func (u *UI) chatThemeOf(id string) int { return u.lookOf(id).scheme }

// setChatTheme gives a chat one of the Themes: its scheme, and the
// wallpaper that goes with it.
func (u *UI) setChatTheme(id string, i int) { u.setLook(id, chatLook{scheme: i}) }

// setBubbles changes a chat's bubbles and keeps its wallpaper.
func (u *UI) setBubbles(id string, i int) {
	l := u.lookOf(id)
	if l.wall == "" {
		l.wall = "default"
		if t := chatThemes[l.scheme]; t.id != "" && t.kind != minimal {
			l.wall = "theme:" + t.base
		} else if t.kind == minimal {
			l.wall = "plain"
		}
	}
	l.scheme = i
	if t := chatThemes[i]; l.wall == "default" && i == 0 || l.wall == "theme:"+t.base && t.kind != minimal ||
		l.wall == "plain" && t.kind == minimal {
		l.wall = "" // what the scheme has anyway
	}
	u.setLook(id, l)
}

// setWall changes a chat's wallpaper and keeps its bubbles.
func (u *UI) setWall(id, wall string) {
	l := u.lookOf(id)
	l.wall = wall
	u.setLook(id, l)
}

// colors returns a scheme's colors in the current mode.
func (t chatTheme) colors(dark bool) themeColors {
	if dark {
		return t.dark
	}
	return t.light
}

// themeWall returns the wallpaper of theme base: its vibrant scheme's.
func themeWall(base string) (themeColors, bool) {
	for _, t := range chatThemes {
		if t.base == base && t.kind == vibrant {
			return t.light, true
		}
	}
	return themeColors{}, false
}

// wallLook is a wallpaper as the conversation draws it.
type wallLook struct {
	bg, doodle color.NRGBA
	plain      bool   // no doodles
	photo      string // a photo's path, drawn over bg
	focus      f32.Point
}

// wallOf returns the wallpaper of a look on palette p (the scheme's); app
// is the app's palette, whose wallpaper is the default one.
func wallOf(p, app *Palette, dark bool, l chatLook) wallLook {
	def := wallLook{bg: p.ChatBg, doodle: p.Doodle, plain: p.Plain}
	kind, arg, _ := strings.Cut(l.wall, ":")
	switch kind {
	case "":
		return def
	case "default":
		return wallLook{bg: app.ChatBg, doodle: app.Doodle}
	case "plain":
		bg := app.Panel // white, or the dark theme's panel
		return wallLook{bg: bg, doodle: bg, plain: true}
	case "theme":
		for _, t := range chatThemes {
			if t.base == arg && t.kind == vibrant {
				c := t.colors(dark)
				return wallLook{bg: rgb(c.wall), doodle: rgb(c.doodle)}
			}
		}
	case "color":
		if i, err := strconv.Atoi(arg); err == nil && i >= 0 && i < len(solidWalls) {
			return solidLook(solidWalls[i], dark)
		}
	case "photo":
		if file, focus := parsePhotoWall(arg); wallpaperDir != "" && file != "" {
			return wallLook{bg: def.bg, doodle: def.bg, plain: true, photo: filepath.Join(wallpaperDir, filepath.Base(file)), focus: focus}
		}
	}
	return def
}

// solidLook is a plain color wallpaper: dark doodles on a light color and
// light ones on a dark color, as WhatsApp's doodle assets, at the color's
// opacity (in dark mode a third of it, at least 0.1). The assets aren't
// solid ink, so the doodles are pre-mixed at 60% of that.
func solidLook(w solidWall, dark bool) wallLook {
	c, o := w.light, w.doodle
	if dark {
		c, o = w.dark, max(0.1, o*0.35)
	}
	bg := rgb(c)
	ink := rgb(0xffffff)
	if 299*int(bg.R)+587*int(bg.G)+114*int(bg.B) > 140*1000 {
		ink = rgb(0x000000)
	}
	return wallLook{bg: bg, doodle: mix(bg, ink, o*0.6)}
}

// themed returns the palette with a scheme's colors.
func themed(p *Palette, dark bool, t chatTheme) *Palette {
	if t.id == "" {
		return p
	}
	c := t.colors(dark)
	q := *p
	q.ChatBg, q.Doodle, q.Plain = rgb(c.wall), rgb(c.doodle), c.doodle == 0
	q.BubbleOut, q.BubbleIn = rgb(c.out), rgb(c.in)
	q.MetaIn, q.QuoteIn = rgb(c.metaIn), rgb(c.quoteIn)
	q.QuoteOut, q.MetaOut, q.SecondaryOut = rgb(c.quoteOut), rgb(c.metaOut), rgb(c.metaOut)
	q.TextOut, q.LinkOut, q.TickReadOut = rgb(c.textOut), rgb(c.linkOut), rgb(c.readOut)
	q.Green, q.Link, q.TickRead = rgb(c.accent), rgb(c.link), rgb(c.read)
	return &q
}

// lookPal is the palette of the last look drawn, kept so a frame doesn't
// copy the palette again.
type lookPal struct {
	look chatLook
	base *Palette
	pal  *Palette
}

// lookPalette returns the palette a look draws with.
func (u *UI) lookPalette(l chatLook) *Palette {
	if c := u.lookPal; c.pal != nil && c.look == l && c.base == u.pal {
		return c.pal
	}
	p := themed(u.pal, u.dark, chatThemes[l.scheme])
	if l.wall != "" {
		w := wallOf(p, u.appPal, u.dark, l)
		if p == u.pal {
			q := *p
			p = &q
		}
		p.ChatBg, p.Doodle, p.Plain, p.WallPhoto, p.WallFocus = w.bg, w.doodle, w.plain, w.photo, w.focus
	}
	u.lookPal = lookPal{l, u.pal, p}
	return p
}

// chatPalette is the palette the open chat draws with.
func (u *UI) chatPalette(c *model.Chat) *Palette {
	if c == nil {
		return u.pal
	}
	return u.lookPalette(u.lookOf(c.ID))
}

// layoutWallpaper fills the conversation's background: a photo, or the
// color and doodles.
func (u *UI) layoutWallpaper(gtx C) {
	p := u.pal
	t := &u.themeUI
	t.pane = gtx.Constraints.Max
	if p.WallPhoto != "" {
		focus := p.WallFocus
		if t.drag && t.dragPath == p.WallPhoto {
			focus = t.dragFocus
		}
		if u.layoutWallPhoto(gtx, p.WallPhoto, focus) {
			if u.dark {
				// WhatsApp dims a photo in dark mode.
				fillRect(gtx, image.Rectangle{Max: gtx.Constraints.Max}, argb(0x000000, 0x40))
			}
			return
		}
	}
	u.conv.wallpaper.layout(gtx, p.ChatBg, p.Doodle, u.doodles && !p.Plain)
}

// The Chat theme dialog.

// themeUI is the open Chat theme dialog's page.
type themeUI struct {
	page    string // "", "bubble", "wall", "color" or "pos"
	picking bool   // the photo dialog is open
	picked  chan pickedWall
	// The Photo position page: the photo's size there, and the focus
	// being dragged (drawn by the wallpaper too, saved on release).
	posSize   image.Point
	drag      bool
	dragPath  string
	dragFocus f32.Point
	// pane is the size of the conversation's wallpaper, drawn last, and
	// wall the photo it showed (wallphoto.go).
	pane image.Point
	wall photoWall
}

type pickedWall struct {
	chatID, path string
	err          error
}

func (u *UI) openChatTheme(c *model.Chat) {
	id := lookAll
	if c != nil {
		id = c.ID
	}
	u.themeUI.page = ""
	u.dialog = dialogState{kind: dialogTheme, title: id}
}

// pickWallPhoto opens the system's file dialog for a photo to be chat
// id's wallpaper.
func (u *UI) pickWallPhoto(id string) {
	t := &u.themeUI
	if t.picking || wallpaperDir == "" {
		return
	}
	if t.picked == nil {
		t.picked = make(chan pickedWall, 1)
	}
	t.picking = true
	out, notify := t.picked, u.images.invalidate
	go func() {
		paths, err := filepick.Open("Choose a wallpaper", false, filepick.Filter{Name: "Photos", Exts: photoExts})
		r := pickedWall{chatID: id, err: err}
		if len(paths) > 0 {
			r.path, r.err = keepWallPhoto(paths[0])
		}
		out <- r
		if notify != nil {
			notify()
		}
	}()
}

// keepWallPhoto copies a photo into wallpaperDir, named by its contents,
// and returns its name there.
func keepWallPhoto(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	name := hex.EncodeToString(sum[:12]) + strings.ToLower(filepath.Ext(path))
	if err := os.MkdirAll(wallpaperDir, 0o755); err != nil {
		return "", err
	}
	return name, os.WriteFile(filepath.Join(wallpaperDir, name), b, 0o644)
}

// updateWallPick takes the photo the dialog returned.
func (u *UI) updateWallPick() {
	t := &u.themeUI
	select {
	case r := <-t.picked:
		t.picking = false
		switch {
		case r.err == filepick.ErrUnsupported:
			u.toast("No file dialog found. Install xdg-desktop-portal (or zenity) to choose a photo.")
		case r.err != nil:
			u.toast("Couldn't use that photo.")
		case r.path != "":
			// The photo's position next, from the middle.
			u.setWall(r.chatID, photoWallValue(r.path, wallFocusCenter))
			u.themeUI.page = "pos"
			u.dialog.list.Position = layout.Position{}
		}
	default:
	}
}

// themePanel is the Chat theme dialog: the Themes, and the Chat bubble,
// Wallpaper and Color pages. A pick shows at once.
func (u *UI) themePanel(gtx C) D {
	d := &u.dialog
	p := u.pal
	t := &u.themeUI
	id := d.title
	u.updateWallPick()
	if u.btn("theme:done").Clicked(gtx) {
		u.closeDialog()
	}
	if u.btn("theme:back").Clicked(gtx) {
		t.page = map[string]string{"color": "wall", "pos": "wall"}[t.page]
		d.list.Position = layout.Position{}
	}
	for _, pg := range []string{"bubble", "wall", "color"} {
		if u.btn("theme:go:" + pg).Clicked(gtx) {
			t.page = pg
			d.list.Position = layout.Position{}
		}
	}
	if u.btn("theme:gallery").Clicked(gtx) {
		u.pickWallPhoto(id)
	}
	d.list.Axis = layout.Vertical
	tile, gap := gtx.Dp(82), gtx.Dp(8)
	const cols = 5
	w := min(cols*tile+(cols-1)*gap+gtx.Dp(48), gtx.Constraints.Max.X-gtx.Dp(32))
	h := min(gtx.Dp(640), gtx.Constraints.Max.Y-gtx.Dp(48))
	gtx.Constraints = layout.Exact(image.Pt(w, h))
	inner := w - gtx.Dp(48)

	var rows []layout.Widget
	title := "Chat theme"
	switch t.page {
	case "bubble":
		title, rows = "Chat bubble", u.bubbleRows(gtx, id, inner)
	case "wall":
		title, rows = "Wallpaper", u.wallRows(gtx, id, inner)
	case "color":
		title, rows = "Color", u.colorRows(gtx, id, inner)
	case "pos":
		title, rows = "Photo position", u.wallPosRows(gtx, id, inner)
	default:
		if id == lookAll {
			title = "Chat theme for all chats"
		}
		rows = u.themeRows(gtx, id, inner)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 12, Right: 24, Top: 16, Bottom: 8}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						if t.page == "" {
							return D{Size: image.Pt(gtx.Dp(12), 0)}
						}
						return u.iconButton(gtx, u.btn("theme:back"), icBack, 40, 24, p.Icon)
					}),
					layout.Rigid(u.label(19.5, title, p.Text, labelOpts{weight: font.Medium}).Layout),
				)
			})
		}),
		layout.Flexed(1, func(gtx C) D {
			return u.scrollList(gtx, &d.list, len(rows), func(gtx C, i int) D {
				return layout.Inset{Left: 24, Right: 24}.Layout(gtx, rows[i])
			})
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 24, Right: 24, Top: 12, Bottom: 20}.Layout(gtx, func(gtx C) D {
				return layout.E.Layout(gtx, func(gtx C) D {
					return u.dialogButton(gtx, "theme:done", dialogButton{label: "Done", primary: true})
				})
			})
		}),
	)
}

// heading is a section's title in the dialog.
func (u *UI) themeHeading(s string) layout.Widget {
	return func(gtx C) D {
		return layout.Inset{Top: 8, Bottom: 12}.Layout(gtx, u.label(15, s, u.pal.TextSecondary, labelOpts{weight: font.Medium}).Layout)
	}
}

// grid lays n cells of size cell out in rows of as many as fit in width w,
// drawing each with draw; it returns the rows.
func grid(n int, cell image.Point, gap, w int, draw func(gtx C, i int)) []layout.Widget {
	cols := max(1, (w+gap)/(cell.X+gap))
	var rows []layout.Widget
	for from := 0; from < n; from += cols {
		to := min(from+cols, n)
		rows = append(rows, func(gtx C) D {
			for i := from; i < to; i++ {
				t := op.Offset(image.Pt((i-from)*(cell.X+gap), 0)).Push(gtx.Ops)
				cg := gtx
				cg.Constraints = layout.Exact(cell)
				draw(cg, i)
				t.Pop()
			}
			return D{Size: image.Pt(gtx.Constraints.Max.X, cell.Y+gap)}
		})
	}
	return rows
}

// themeRows is the dialog's first page: Chat bubble and Wallpaper, then
// WhatsApp's Themes (Default and the themes without a photo wallpaper,
// then those with one, drawn in their colors).
func (u *UI) themeRows(gtx C, id string, w int) []layout.Widget {
	l := u.lookOf(id)
	lp := u.lookPalette(l)
	rows := []layout.Widget{func(gtx C) D {
		return layout.Inset{Top: 4, Bottom: 16}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Spacing: layout.SpaceAround}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return u.themeAction(gtx, "theme:go:bubble", "Chat bubble", func(gtx C, r image.Rectangle) {
						b := image.Rectangle{Min: r.Min.Add(image.Pt(r.Dx()/2-gtx.Dp(18), r.Dy()/2-gtx.Dp(11))), Max: r.Min.Add(image.Pt(r.Dx()/2+gtx.Dp(18), r.Dy()/2+gtx.Dp(11)))}
						fillRRect(gtx, b, gtx.Dp(6), lp.BubbleOut)
					})
				}),
				layout.Rigid(func(gtx C) D {
					return u.themeAction(gtx, "theme:go:wall", "Wallpaper", func(gtx C, r image.Rectangle) {
						b := image.Rectangle{Min: r.Min.Add(image.Pt(r.Dx()/2-gtx.Dp(12), r.Dy()/2-gtx.Dp(16))), Max: r.Min.Add(image.Pt(r.Dx()/2+gtx.Dp(12), r.Dy()/2+gtx.Dp(16)))}
						fillRRect(gtx, b, gtx.Dp(5), lp.ChatBg)
					})
				}),
			)
		})
	}}
	rows = append(rows, u.themeHeading("Themes"))
	var plain, stock []int
	for i, t := range chatThemes {
		switch {
		case i == 0, !t.stock:
			plain = append(plain, i)
		case t.kind == vibrant:
			stock = append(stock, i)
		}
	}
	cur := -1
	if l.wall == "" {
		cur = l.scheme
	}
	cell, gap := image.Pt(gtx.Dp(82), gtx.Dp(82)), gtx.Dp(8)
	for _, set := range [][]int{plain, stock} {
		rows = append(rows, grid(len(set), cell, gap, w, func(gtx C, j int) {
			i := set[j]
			u.themeSwatch(gtx, id, i, i == cur)
		})...)
		rows = append(rows, func(gtx C) D { return D{Size: image.Pt(0, gtx.Dp(12))} })
	}
	return append(rows, func(gtx C) D {
		return layout.Inset{Bottom: 8}.Layout(gtx, u.label(13.5, "The chat color and wallpaper will both change.", u.pal.TextSecondary).Layout)
	})
}

// themeAction is a big button of the dialog's: a rounded box with art in
// it, and its name under it.
func (u *UI) themeAction(gtx C, key, label string, art func(gtx C, r image.Rectangle)) D {
	p := u.pal
	c := u.btn(key)
	return clickable(gtx, c, func(gtx C) D {
		bw, bh := gtx.Dp(120), gtx.Dp(76)
		r := image.Rect(0, 0, bw, bh)
		fillRRect(gtx, r, bh/2, mix(p.Search, p.Hover, u.hover(gtx, c)))
		art(gtx, r)
		l := record(gtx, u.label(14, label, p.Text, labelOpts{maxLines: 1}).Layout)
		l.at(gtx, (bw-l.size.X)/2, bh+gtx.Dp(8))
		return D{Size: image.Pt(bw, bh+gtx.Dp(8)+l.size.Y)}
	})
}

// themeSwatch is one of the Themes as WhatsApp's picker draws it
// (WAWebChatThemeCanvas): its wallpaper, with a bubble of theirs and one
// of yours at the bottom, framed in the accent when it's the chat's.
func (u *UI) themeSwatch(gtx C, id string, i int, on bool) D {
	p := u.pal
	tp := themed(p, u.dark, chatThemes[i])
	c := u.btn("theme:" + itoa(i))
	if c.Clicked(gtx) {
		u.setChatTheme(id, i)
	}
	return clickable(gtx, c, func(gtx C) D {
		sz := gtx.Constraints.Max
		u.swatchFrame(gtx, sz, on, u.hover(gtx, c), tp.ChatBg)
		bub := image.Pt(gtx.Dp(36), gtx.Dp(10))
		pad, gap := gtx.Dp(8), gtx.Dp(2)
		out := image.Rectangle{Min: image.Pt(sz.X-pad-bub.X, sz.Y-gtx.Dp(4)-bub.Y), Max: image.Pt(sz.X-pad, sz.Y-gtx.Dp(4))}
		in := image.Rectangle{Min: image.Pt(pad, out.Min.Y-gap-bub.Y), Max: image.Pt(pad+bub.X, out.Min.Y-gap)}
		fillRRect(gtx, in, gtx.Dp(4), tp.BubbleIn)
		fillRRect(gtx, out, gtx.Dp(4), tp.BubbleOut)
		return D{Size: sz}
	})
}

// swatchFrame draws a tile's background in bg, framed 1dp, or 3dp in the
// accent when on.
func (u *UI) swatchFrame(gtx C, sz image.Point, on bool, hover float32, bg color.NRGBA) {
	p := u.pal
	r := image.Rectangle{Max: sz}
	rad := gtx.Dp(12)
	ring, bw := mix(p.ChipBorder, p.TextSecondary, hover), gtx.Dp(1)
	if on {
		ring, bw = p.Green, gtx.Dp(3)
	}
	fillRRect(gtx, r, rad, ring)
	fillRRect(gtx, r.Inset(bw), rad-bw, bg)
}

// circle is a round swatch of the Chat bubble, Color and App theme pages,
// ringed with a check in it when on.
func (u *UI) circle(gtx C, key string, col, bg color.NRGBA, on bool) (D, bool) {
	p := u.pal
	c := u.btn(key)
	clicked := c.Clicked(gtx)
	return clickable(gtx, c, func(gtx C) D {
		sz := gtx.Constraints.Max
		ctr := image.Pt(sz.X/2, sz.Y/2)
		r := min(sz.X, sz.Y)/2 - gtx.Dp(4)
		if on {
			fillCircle(gtx, ctr, r+gtx.Dp(4), p.Text)
			fillCircle(gtx, ctr, r+gtx.Dp(2), bg)
			r -= gtx.Dp(2)
		} else if h := u.hover(gtx, c); h > 0 {
			fillCircle(gtx, ctr, r+gtx.Dp(2), faded(p.TextSecondary, h))
		}
		fillCircle(gtx, ctr, r, col)
		if on {
			fillCircle(gtx, ctr, gtx.Dp(12), rgb(0xffffff))
			s := gtx.Dp(18)
			t := op.Offset(ctr.Sub(image.Pt(s/2, s/2))).Push(gtx.Ops)
			drawIcon(gtx, icTick, unit.Dp(18), rgb(0x0a0a0a))
			t.Pop()
		}
		return D{Size: sz}
	}), clicked
}

// bubbleRows is the Chat bubble page: every theme's vibrant and tonal
// bubbles, as the Android app lists them, then the minimal ones.
func (u *UI) bubbleRows(gtx C, id string, w int) []layout.Widget {
	l := u.lookOf(id)
	cur := l.scheme
	if cur == 0 {
		cur = schemeIndex("WhatsApp-Green@Tonal") // the app's own bubbles
	}
	var set []int
	for _, base := range bubbleThemes {
		set = append(set, schemeIndex(base), schemeIndex(base+"@Tonal"))
	}
	cell, gap := image.Pt(gtx.Dp(72), gtx.Dp(72)), gtx.Dp(12)
	// An even number of columns keeps each theme's two side by side.
	cols := max(2, (w+gap)/(cell.X+gap)&^1)
	return grid(len(set), cell, gap, cols*(cell.X+gap)-gap, func(gtx C, j int) {
		i := set[j]
		if _, clicked := u.circle(gtx, "bubble:"+itoa(i), rgb(chatThemes[i].colors(u.dark).out), u.pal.Dialog, i == cur); clicked {
			if i == schemeIndex("WhatsApp-Green@Tonal") {
				i = 0
			}
			u.setBubbles(id, i)
		}
	})
}

// wallRows is the Wallpaper page: a photo of yours or a plain color, then
// the default wallpaper and every theme's.
func (u *UI) wallRows(gtx C, id string, w int) []layout.Widget {
	l := u.lookOf(id)
	cur := l.wall
	if cur == "" {
		t := chatThemes[l.scheme]
		switch {
		case l.scheme == 0:
			cur = "default"
		case t.kind == minimal:
			cur = "plain"
		default:
			cur = "theme:" + t.base
		}
	}
	rows := []layout.Widget{func(gtx C) D {
		return layout.Inset{Top: 4, Bottom: 16}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Spacing: layout.SpaceAround}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return u.themeAction(gtx, "theme:gallery", "Photo", func(gtx C, r image.Rectangle) {
						t := op.Offset(r.Min.Add(image.Pt(r.Dx()/2-gtx.Dp(14), r.Dy()/2-gtx.Dp(14)))).Push(gtx.Ops)
						drawIcon(gtx, icPhotoLibrary, 28, u.pal.IconStrong)
						t.Pop()
					})
				}),
				layout.Rigid(func(gtx C) D {
					return u.themeAction(gtx, "theme:go:color", "Color", func(gtx C, r image.Rectangle) {
						t := op.Offset(r.Min.Add(image.Pt(r.Dx()/2-gtx.Dp(14), r.Dy()/2-gtx.Dp(14)))).Push(gtx.Ops)
						drawIcon(gtx, icPalette, 28, u.pal.IconStrong)
						t.Pop()
					})
				}),
			)
		})
	}}
	walls := []string{"default", "plain"}
	if strings.HasPrefix(cur, "photo:") || strings.HasPrefix(cur, "color:") {
		walls = append([]string{cur}, walls...)
	}
	seen := map[string]bool{}
	for _, t := range chatThemes {
		if t.id != "" && !seen[t.base] {
			seen[t.base] = true
			walls = append(walls, "theme:"+t.base)
		}
	}
	rows = append(rows, u.themeHeading("Wallpapers"))
	cell := image.Pt(gtx.Dp(82), gtx.Dp(118))
	rows = append(rows, grid(len(walls), cell, gtx.Dp(8), w, func(gtx C, i int) {
		wall := walls[i]
		c := u.btn("wall:" + wall)
		if c.Clicked(gtx) {
			if wall == cur && strings.HasPrefix(wall, "photo:") {
				// The chosen photo again: its position.
				u.themeUI.page = "pos"
				u.dialog.list.Position = layout.Position{}
			} else {
				u.setWall(id, wall)
			}
		}
		lp := wallOf(u.pal, u.appPal, u.dark, chatLook{wall: wall})
		clickable(gtx, c, func(gtx C) D {
			sz := gtx.Constraints.Max
			u.swatchFrame(gtx, sz, wall == cur, u.hover(gtx, c), lp.bg)
			if lp.photo != "" {
				if e := u.wallThumb(lp.photo); e.state == imgReady {
					in := image.Rectangle{Max: sz}.Inset(gtx.Dp(3))
					paintFocused(gtx, e.op, e.size, in, lp.focus)
				}
				if wall == cur {
					// A click opens its position: a pencil says so.
					ctr := image.Pt(sz.X-gtx.Dp(18), sz.Y-gtx.Dp(18))
					fillCircle(gtx, ctr, gtx.Dp(12), argb(0x000000, 0x99))
					t := op.Offset(ctr.Sub(image.Pt(gtx.Dp(8), gtx.Dp(8)))).Push(gtx.Ops)
					drawIcon(gtx, icEdit, 16, rgb(0xffffff))
					t.Pop()
				}
			} else if !lp.plain && u.doodles {
				// Rings in the doodles' color hint at the pattern.
				step := gtx.Dp(20)
				for y := step; y < sz.Y-step/2; y += step {
					for x := step / 2 * (1 + y/step%2); x < sz.X-step/3; x += step {
						fillCircle(gtx, image.Pt(x, y), gtx.Dp(4), lp.doodle)
						fillCircle(gtx, image.Pt(x, y), gtx.Dp(2), lp.bg)
					}
				}
			}
			return D{Size: sz}
		})
	})...)
	return rows
}

// colorRows is the Color page: the plain color wallpapers.
func (u *UI) colorRows(gtx C, id string, w int) []layout.Widget {
	cur := u.lookOf(id).wall
	cell := image.Pt(gtx.Dp(72), gtx.Dp(72))
	return grid(len(solidWalls), cell, gtx.Dp(12), w, func(gtx C, i int) {
		key := "color:" + itoa(i)
		if _, clicked := u.circle(gtx, key, solidLook(solidWalls[i], u.dark).bg, u.pal.Dialog, cur == key); clicked {
			u.setWall(id, key)
		}
	})
}
