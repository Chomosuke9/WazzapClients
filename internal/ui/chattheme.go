package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/filepick"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

// Chat themes, as WhatsApp has them (WhatsApp Web's ChatTheme*.stylex and
// WAWebChatThemeDrawer, the Android app's Chat theme pages): a chat's look
// is a color scheme for its bubbles and accent, and a wallpaper. Picking
// one of the Themes sets both (the wallpaper follows the scheme); Chat
// bubble changes the bubbles alone and Wallpaper the wallpaper alone: a
// theme's, a plain color (with or without doodles) or a photo of yours.
// Every chat takes the look set for all chats (Settings > Chats > Default
// chat theme) until it gets one of its own. The tables are in chatthemes.go.

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
// wallpaperDir>". A third part says the wallpaper's doodles: "bare" draws
// none and "doodles" some, whatever prefDoodles says. A chat without a
// value takes lookAll's; "|" is a chat set back to the app's own look.
const (
	prefChatTheme = "theme:"
	lookAll       = "*"
)

// chatLook is a chat's scheme (an index in chatThemes) and wallpaper.
type chatLook struct {
	scheme int
	wall   string // "" follows the scheme
	// The doodles on the wallpaper: none (bare), some (doodles), or as
	// prefDoodles says (neither).
	bare, doodles bool
}

// bareWall reports whether look l draws no doodles: as it says itself, or
// as prefDoodles does.
func (u *UI) bareWall(l chatLook) bool { return l.bare || !l.doodles && !u.doodles }

// setDoodles turns the wallpaper's doodles on or off in every chat whose
// own theme doesn't say: the Default chat theme's switch, and the
// Performance page's.
func (u *UI) setDoodles(on bool) {
	setPref(u.backend, prefDoodles, on)
	u.doodles = on
	if l := u.lookOf(lookAll); l.bare || l.doodles {
		l.bare, l.doodles = false, false // older versions kept it there
		u.setLook(lookAll, l)
	}
	u.lookPal = lookPal{}
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
		wall, doodles, _ := strings.Cut(wall, "|")
		l = chatLook{scheme: schemeIndex(scheme), wall: wall,
			bare: doodles != "" && doodles != "doodles", doodles: doodles == "doodles"}
	}
	u.themes[id] = l
	return l
}

// setLook keeps chat id's look.
func (u *UI) setLook(id string, l chatLook) {
	v := chatThemes[l.scheme].id
	if l.wall != "" || l.bare || l.doodles {
		v += "|" + l.wall
	}
	switch {
	case l.bare:
		v += "|bare"
	case l.doodles:
		v += "|doodles"
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
func (u *UI) setChatTheme(id string, i int) {
	l := u.lookOf(id)
	u.setLook(id, chatLook{scheme: i, bare: l.bare, doodles: l.doodles})
}

// resetLook gives chat id the default look again: every chat's, or for
// lookAll the app's own.
func (u *UI) resetLook(id string) {
	if id == lookAll {
		u.setDoodles(true)
	}
	u.backend.SetPref(prefChatTheme+id, "")
	u.themes = nil
	u.lookPal = lookPal{}
}

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

// wallOf returns the wallpaper of a look on palette p (the scheme's). The
// default one is the app's palette's, and under a palette (scheme.go) so is
// a theme's, whose colors are WhatsApp's.
func (u *UI) wallOf(p *Palette, l chatLook) wallLook {
	app, dark := u.appPal, u.dark
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
		if schemeOf(u.scheme) != nil {
			return wallLook{bg: app.ChatBg, doodle: app.Doodle}
		}
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

// themeHues are the hues a palette (scheme.go) gives a chat with one of
// WhatsApp's themes, by the theme: the first its flavor has. The grey
// themes, and one without a hue here, keep the palette's accent.
var themeHues = map[string][]string{
	"WhatsApp-Green": {"green"}, "Lime": {"green"}, "Charcoal-Green": {"green", "teal"},
	"Emerald": {"teal", "green"}, "Merino-Teal": {"teal", "cyan", "green"},
	"Sky-Blue": {"cyan", "sapphire", "blue"}, "Dark-Cerulean": {"sapphire", "blue"},
	"Royal-Blue": {"blue"}, "Default-Blue": {"blue"},
	"Pearl-Indigo": {"lavender", "purple", "blue"}, "Tyrian-Purple": {"purple", "pink"},
	"Pink": {"pink", "purple", "red"}, "Persian-Plum": {"maroon", "red"}, "Red": {"red"},
	"Sunrise-Orange": {"orange", "red"}, "Sunset-Orange": {"flamingo", "orange"}, "Brown": {"orange", "sand"},
	"Cream": {"rosewater", "sand", "yellow"}, "Warm-Yellow": {"yellow", "gold"}, "Lemon": {"gold", "yellow"},
}

// themed returns the app's palette with a chat theme's colors: WhatsApp's,
// or under a palette that palette with the theme's hue as its accent, so
// the bubbles stay the palette's.
func (u *UI) themed(t chatTheme) *Palette {
	s := schemeOf(u.scheme)
	if s == nil || t.id == "" {
		return themed(u.pal, u.dark, t)
	}
	f := s.flavor(u.dark)
	for _, id := range themeHues[t.base] {
		if _, ok := f.hue(id); ok {
			return appPalette(u.dark, u.scheme, "", id)
		}
	}
	return u.pal
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
	p := u.themed(chatThemes[l.scheme])
	if bare := u.bareWall(l); l.wall != "" || bare {
		w := u.wallOf(p, l)
		q := *p // p may be the app's palette, or a palette's kept one
		p = &q
		p.ChatBg, p.Doodle, p.Plain, p.WallPhoto, p.WallFocus = w.bg, w.doodle, w.plain || bare, w.photo, w.focus
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
	u.conv.wallpaper.layout(gtx, p.ChatBg, p.Doodle, !p.Plain)
}

// Chat theme: a panel beside the open chat, in the info panel's place,
// like WhatsApp's; every chat's (Settings) is a dialog of the same pages.

// themeUI is the open Chat theme panel or dialog's page.
type themeUI struct {
	open     bool // the panel, for chatID
	chatID   string
	fromInfo bool // opened from the info panel: closing goes back to it
	anim     tween
	list     widget.List
	page     string // "", "all", "bubble", "wall", "color" or "pos"
	picking  bool   // the photo dialog is open
	picked   chan pickedWall
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

// shown reports whether the panel is open or still sliding away.
func (t *themeUI) shown() bool { return t.open || t.anim.v > 0 }

// openChatTheme opens c's Chat theme panel in place of the info, search or
// Message info panel, or with c nil the dialog of every chat's theme.
func (u *UI) openChatTheme(c *model.Chat) {
	t := &u.themeUI
	u.themeGo("")
	if c == nil {
		u.dialog = dialogState{kind: dialogTheme, title: lookAll}
		return
	}
	t.fromInfo = u.info.open && u.info.chatID == c.ID
	if u.info.open || u.search.open || u.msgInfo.open {
		u.hideInfo()
		u.hideChatSearch()
		u.hideMsgInfo()
		t.anim.snap(true)
	}
	t.open, t.chatID = true, c.ID
}

// hideChatTheme closes the panel at once, for when the chat or page
// changes under it, or another panel takes its place.
func (u *UI) hideChatTheme() {
	u.themeUI.open = false
	u.themeUI.anim.snap(false)
}

// themeGo opens a page of the Chat theme panel or dialog.
func (u *UI) themeGo(page string) {
	u.themeUI.page = page
	u.themeUI.list.Position = layout.Position{}
	u.dialog.list.Position = layout.Position{}
}

// themeBack goes back a page, or from the first one closes the panel (to
// the info panel it was opened from).
func (u *UI) themeBack() {
	t := &u.themeUI
	if t.page != "" {
		u.themeGo(map[string]string{"color": "wall", "pos": "wall"}[t.page])
		return
	}
	if !t.open {
		return
	}
	if t.fromInfo {
		u.hideChatTheme()
		u.openInfo(t.chatID)
		u.info.anim.snap(true)
		return
	}
	t.open = false
}

// themeBg is the background the theme pages are drawn on.
func (u *UI) themeBg() color.NRGBA {
	if u.themeUI.open {
		return u.pal.Panel
	}
	return u.pal.Dialog
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
			u.themeGo("pos")
		}
	default:
	}
}

// themePages handles the pages' buttons and returns the open page's title
// and rows, laid out for width w.
func (u *UI) themePages(gtx C, id string, w int) (string, []layout.Widget) {
	t := &u.themeUI
	u.updateWallPick()
	if u.btn("theme:back").Clicked(gtx) {
		u.themeBack()
	}
	if u.btn("theme:reset").Clicked(gtx) {
		u.resetLook(id)
		u.themeGo("")
	}
	for _, pg := range []string{"all", "bubble", "wall", "color"} {
		if u.btn("theme:go:" + pg).Clicked(gtx) {
			u.themeGo(pg)
		}
	}
	if u.btn("theme:gallery").Clicked(gtx) {
		u.pickWallPhoto(id)
	}
	switch t.page {
	case "all":
		return "Themes", u.allThemeRows(gtx, id, w)
	case "bubble":
		return "Chat bubble", u.bubbleRows(gtx, id, w)
	case "wall":
		return "Wallpaper", u.wallRows(gtx, id, w)
	case "color":
		return "Color", u.colorRows(gtx, id, w)
	case "pos":
		return "Photo position", u.wallPosRows(gtx, id, w)
	}
	if id == lookAll {
		return "Default chat theme", u.themeRows(gtx, id, w)
	}
	return "Chat theme", u.themeRows(gtx, id, w)
}

// themeReset is the header's button that gives the default look back.
func (u *UI) themeReset(gtx C) D {
	return u.iconButton(gtx, u.btn("theme:reset"), icRefresh, 40, 24, u.pal.Icon)
}

// layoutChatTheme is the Chat theme panel beside the chat: a back arrow,
// the page's title and the reset button, then the page. A pick shows at
// once in the chat.
func (u *UI) layoutChatTheme(gtx C) D {
	p := u.pal
	t := &u.themeUI
	dims := fill(gtx, p.Panel)
	t.list.Axis = layout.Vertical
	title, rows := u.themePages(gtx, t.chatID, gtx.Constraints.Max.X-gtx.Dp(48))
	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return vcenter(gtx, gtx.Dp(64), func(gtx C) D {
				return layout.Inset{Left: 11.5, Right: 14}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return u.iconButton(gtx, u.btn("theme:back"), icBack, 40, 24, p.IconStrong) }),
						layout.Rigid(layout.Spacer{Width: 10}.Layout),
						layout.Flexed(1, u.label(16.5, title, p.Text, labelOpts{maxLines: 1}).Layout),
						layout.Rigid(u.themeReset),
					)
				})
			})
		}),
		layout.Rigid(layout.Spacer{Height: 12}.Layout),
		layout.Flexed(1, func(gtx C) D {
			return u.scrollList(gtx, &t.list, len(rows), func(gtx C, i int) D {
				return layout.Inset{Left: 24, Right: 24}.Layout(gtx, rows[i])
			})
		}),
	)
	return dims
}

// themePanel is the Chat theme dialog (every chat's theme): the pages the
// panel shows.
func (u *UI) themePanel(gtx C) D {
	d := &u.dialog
	p := u.pal
	t := &u.themeUI
	id := d.title
	if u.btn("theme:done").Clicked(gtx) {
		u.closeDialog()
	}
	d.list.Axis = layout.Vertical
	tile, gap := gtx.Dp(82), gtx.Dp(8)
	const cols = 5
	w := min(cols*tile+(cols-1)*gap+gtx.Dp(48), gtx.Constraints.Max.X-gtx.Dp(32))
	h := min(gtx.Dp(640), gtx.Constraints.Max.Y-gtx.Dp(48))
	gtx.Constraints = layout.Exact(image.Pt(w, h))
	title, rows := u.themePages(gtx, id, w-gtx.Dp(48))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 12, Right: 16, Top: 16, Bottom: 8}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						if t.page == "" {
							return D{Size: image.Pt(gtx.Dp(12), 0)}
						}
						return u.iconButton(gtx, u.btn("theme:back"), icBack, 40, 24, p.Icon)
					}),
					layout.Flexed(1, u.label(19.5, title, p.Text, labelOpts{weight: font.Medium, maxLines: 1}).Layout),
					layout.Rigid(u.themeReset),
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

// heading is a section's title on a theme page.
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

// themeSets are WhatsApp's Themes in the order they're offered: Default
// and the themes without a photo wallpaper, then those with one.
func themeSets() (plain, stock []int) {
	for i, t := range chatThemes {
		switch {
		case i == 0, !t.stock:
			plain = append(plain, i)
		case t.kind == vibrant:
			stock = append(stock, i)
		}
	}
	return plain, stock
}

// themeTiles lays the themes set out three to a row.
func (u *UI) themeTiles(gtx C, id string, set []int, w int) []layout.Widget {
	l := u.lookOf(id)
	cur := -1
	if l.wall == "" {
		cur = l.scheme
	}
	gap := gtx.Dp(12)
	cw := (w - 2*gap) / 3
	return grid(len(set), image.Pt(cw, cw*2/3), gap, w, func(gtx C, j int) {
		u.themeSwatch(gtx, id, set[j], set[j] == cur)
	})
}

// themeRows is the first page, as WhatsApp's panel: six Themes and See
// all, the doodles' switch, then Customize: Chat bubble and Wallpaper.
func (u *UI) themeRows(gtx C, id string, w int) []layout.Widget {
	p := u.pal
	l := u.lookOf(id)
	lp := u.lookPalette(l)
	plain, stock := themeSets()
	set := append(append([]int{}, plain[:min(3, len(plain))]...), stock[:min(3, len(stock))]...)
	if l.wall == "" && !slices.Contains(set, l.scheme) && len(set) > 1 {
		set[1] = l.scheme // the chat's own theme is always among them
	}
	if u.btn("theme:doodles").Clicked(gtx) {
		on := u.bareWall(l)
		if id == lookAll {
			u.setDoodles(on)
		} else {
			// As every chat's are, or apart from them.
			l.bare, l.doodles = !on && u.doodles, on && !u.doodles
			u.setLook(id, l)
		}
		l = u.lookOf(id)
	}
	rows := []layout.Widget{func(gtx C) D {
		return layout.Inset{Top: 8, Bottom: 18}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(u.label(16.5, "Themes", p.Text, labelOpts{weight: font.Medium}).Layout),
						layout.Rigid(layout.Spacer{Height: 3}.Layout),
						layout.Rigid(u.label(13.5, "The chat color and wallpaper will both change.", p.TextSecondary, labelOpts{maxLines: 2}).Layout),
					)
				}),
				layout.Rigid(layout.Spacer{Width: 12}.Layout),
				layout.Rigid(func(gtx C) D {
					c := u.btn("theme:go:all")
					return clickable(gtx, c, func(gtx C) D {
						m := op.Record(gtx.Ops)
						d := layout.Inset{Left: 16, Right: 16, Top: 8, Bottom: 8}.Layout(gtx,
							u.label(14, "See all", p.Green, labelOpts{weight: font.Medium, maxLines: 1}).Layout)
						call := m.Stop()
						borderRRect(gtx, image.Rectangle{Max: d.Size}, d.Size.Y/2, mix(u.themeBg(), p.Hover, u.hover(gtx, c)), p.ChipBorder)
						call.Add(gtx.Ops)
						return d
					})
				}),
			)
		})
	}}
	rows = append(rows, u.themeTiles(gtx, id, set, w)...)
	rows = append(rows,
		func(gtx C) D {
			return layout.Inset{Top: 6}.Layout(gtx, func(gtx C) D {
				return u.themeRow(gtx, "theme:doodles", icDoodles, "WhatsApp doodles", func(gtx C) D {
					return u.themeSwitch(gtx, "theme:doodles", !u.bareWall(l))
				})
			})
		},
		func(gtx C) D {
			sz := image.Pt(gtx.Constraints.Max.X, gtx.Dp(41))
			fillRect(gtx, image.Rect(0, gtx.Dp(20), sz.X, gtx.Dp(20)+max(1, gtx.Dp(1))), p.Divider)
			return D{Size: sz}
		},
		func(gtx C) D {
			return layout.Inset{Top: 4, Bottom: 8}.Layout(gtx, u.label(16.5, "Customize", p.Text, labelOpts{weight: font.Medium}).Layout)
		},
		func(gtx C) D {
			return u.themeRow(gtx, "theme:go:bubble", icBubble, "Chat bubble", func(gtx C) D {
				s := gtx.Dp(30)
				fillCircle(gtx, image.Pt(s/2, s/2), s/2, lp.BubbleOut)
				return D{Size: image.Pt(s, s)}
			})
		},
		func(gtx C) D {
			return u.themeRow(gtx, "theme:go:wall", icWallpaper, "Wallpaper", func(gtx C) D {
				s := gtx.Dp(30)
				r := image.Rect(0, 0, s, s)
				fillRRect(gtx, r, gtx.Dp(7), p.ChipBorder)
				fillRRect(gtx, r.Inset(max(1, gtx.Dp(1))), gtx.Dp(6), lp.ChatBg)
				return D{Size: image.Pt(s, s)}
			})
		},
	)
	return rows
}

// allThemeRows is See all's page: every Theme.
func (u *UI) allThemeRows(gtx C, id string, w int) []layout.Widget {
	plain, stock := themeSets()
	return u.themeTiles(gtx, id, append(plain, stock...), w)
}

// themeRow is a row of the first page: an icon, its name, and at the
// right edge a switch or a sample of what the row changes.
func (u *UI) themeRow(gtx C, key string, ic *icon.Icon, label string, trailing layout.Widget) D {
	p := u.pal
	c := u.btn(key)
	return clickable(gtx, c, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		m := op.Record(gtx.Ops)
		d := vcenter(gtx, gtx.Dp(56), func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(iconW(ic, 24, p.Icon)),
				layout.Rigid(layout.Spacer{Width: 22}.Layout),
				layout.Flexed(1, u.label(16, label, p.Text, labelOpts{maxLines: 1}).Layout),
				layout.Rigid(trailing),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
			)
		})
		call := m.Stop()
		if h := u.hover(gtx, c); h > 0 {
			fillRRect(gtx, image.Rectangle{Max: d.Size}, gtx.Dp(10), faded(p.RowHover, h))
		}
		call.Add(gtx.Ops)
		return d
	})
}

// themeSwitch draws a switch, its knob sliding when it changes.
func (u *UI) themeSwitch(gtx C, key string, on bool) D {
	p := u.pal
	v := smooth(u.follows.toggle(gtx, key, on, durHoverIn))
	w, h := gtx.Dp(38), gtx.Dp(22)
	fillRRect(gtx, image.Rect(0, 0, w, h), h/2, mix(p.TextSecondary, p.Green, v))
	r := h/2 - gtx.Dp(3)
	x := lerpInt(h/2, w-h/2, v)
	fillCircle(gtx, image.Pt(x, h/2), r, u.themeBg())
	return D{Size: image.Pt(w, h)}
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
// (WAWebChatThemeCanvas): its wallpaper with a bubble of theirs and one
// of yours on it, ringed with a check when it's the chat's.
func (u *UI) themeSwatch(gtx C, id string, i int, on bool) D {
	p := u.pal
	tp := u.themed(chatThemes[i])
	c := u.btn("theme:" + itoa(i))
	if c.Clicked(gtx) {
		u.setChatTheme(id, i)
	}
	return clickable(gtx, c, func(gtx C) D {
		sz := gtx.Constraints.Max
		r := image.Rectangle{Max: sz}
		rad := gtx.Dp(14)
		if on {
			bw := gtx.Dp(2)
			fillRRect(gtx, r, rad, p.Text)
			fillRRect(gtx, r.Inset(bw), rad-bw, u.themeBg())
			fillRRect(gtx, r.Inset(2*bw), rad-2*bw, tp.ChatBg)
		} else {
			bw := max(1, gtx.Dp(1))
			fillRRect(gtx, r, rad, mix(p.ChipBorder, p.TextSecondary, u.hover(gtx, c)))
			fillRRect(gtx, r.Inset(bw), rad-bw, tp.ChatBg)
		}
		bub := image.Pt(sz.X*27/100, sz.Y*19/100)
		in := image.Pt(sz.X*23/100, sz.Y*16/100)
		out := image.Pt(sz.X*52/100, sz.Y*37/100)
		fillRRect(gtx, image.Rectangle{Min: in, Max: in.Add(bub)}, gtx.Dp(5), tp.BubbleIn)
		fillRRect(gtx, image.Rectangle{Min: out, Max: out.Add(bub)}, gtx.Dp(5), tp.BubbleOut)
		if on {
			ctr := image.Pt(sz.X/2, sz.Y*78/100)
			fillCircle(gtx, ctr, gtx.Dp(10), rgb(0xffffff))
			s := gtx.Dp(16)
			t := op.Offset(ctr.Sub(image.Pt(s/2, s/2))).Push(gtx.Ops)
			drawIcon(gtx, icTick, 16, rgb(0x0a0a0a))
			t.Pop()
		}
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
		col := rgb(chatThemes[i].colors(u.dark).out)
		if schemeOf(u.scheme) != nil {
			col = u.themed(chatThemes[i]).BubbleOut
		}
		if _, clicked := u.circle(gtx, "bubble:"+itoa(i), col, u.themeBg(), i == cur); clicked {
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
				u.themeGo("pos")
			} else {
				u.setWall(id, wall)
			}
		}
		lp := u.wallOf(u.pal, chatLook{wall: wall})
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
			} else if !lp.plain && !u.bareWall(l) {
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
		if _, clicked := u.circle(gtx, key, solidLook(solidWalls[i], u.dark).bg, u.themeBg(), cur == key); clicked {
			u.setWall(id, key)
		}
	})
}
