package ui

import (
	"fmt"
	"image"
	"image/color"
	"path/filepath"
	"strconv"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Themes set for everyone in a chat (model.SharedTheme): one that comes
// goes over the chat's own look, and whichever came last shows. Picking a
// look in the Chat theme panel drops it, and so does its reset button,
// which leaves the default look of every chat. The panel's Everyone rows
// set the chat's look for everyone, or reset it.

// sharedWall prefixes a look's wallpaper that is the photo of a shared
// theme: its path follows, in the backend's keeping.
const sharedWall = "shared:"

// sharedLook is look l with a theme set for everyone over it.
func (u *UI) sharedLook(l chatLook, s *model.SharedTheme) chatLook {
	if s.Reset {
		l = u.lookOf(lookAll)
	}
	if s.Scheme != "" {
		l = l.withBubbles(sharedScheme(s.Scheme))
	}
	switch w := s.Wall; w.Kind {
	case model.WallDefault:
		l.wall = ""
	case model.WallColor:
		l.wall = solidColors{w.Light, w.Dark, w.DoodleLight, w.DoodleDark}.value()
	case model.WallPhoto:
		l.wall = sharedWall + w.Photo
	}
	if k := s.Wall.Kind; k == model.WallDefault || k == model.WallColor {
		l.bare, l.doodles = !s.Wall.Doodles, s.Wall.Doodles
	}
	return l
}

// sharedScheme is the index in chatThemes of the scheme others named id.
// Clients send some WhatsApp Web doesn't have ("Red@Minimal"): those take
// the nearest of the same theme, and an unknown theme WhatsApp's colors.
func sharedScheme(id string) int {
	for {
		if i := schemeIndex(id); i != 0 {
			return i
		}
		at := strings.LastIndexByte(id, '@')
		if at < 0 {
			return 0
		}
		id = id[:at]
	}
}

// solidColors is a plain color wallpaper that isn't one of solidWalls: its
// colors by mode (0xRRGGBB) and its doodles' (0xAARRGGBB, 0 to go with the
// color).
type solidColors struct{ light, dark, doodleLight, doodleDark uint32 }

// value is the colors as a look's wallpaper.
func (w solidColors) value() string {
	return fmt.Sprintf("solid:%06x,%06x,%08x,%08x", w.light, w.dark, w.doodleLight, w.doodleDark)
}

// parseSolidWall reads what value wrote, after its "solid:".
func parseSolidWall(arg string) (w solidColors, ok bool) {
	parts := strings.Split(arg, ",")
	if len(parts) != 4 {
		return w, false
	}
	for i, p := range []*uint32{&w.light, &w.dark, &w.doodleLight, &w.doodleDark} {
		n, err := strconv.ParseUint(parts[i], 16, 32)
		if err != nil {
			return w, false
		}
		*p = uint32(n)
	}
	return w, true
}

// look is the wallpaper in a mode.
func (w solidColors) look(dark bool) wallLook {
	l := solidLook(solidWall{light: w.light, dark: w.dark, doodle: 0.3}, dark)
	ink := w.doodleLight
	if dark {
		ink = w.doodleDark
	}
	if a := ink >> 24; a != 0 {
		l.doodle = mix(l.bg, rgb(ink&0xffffff), float32(a)/255)
	}
	return l
}

// shareable is look l as it's set for everyone: what it shows here, under
// a palette what it shows without one.
func (u *UI) shareable(l chatLook) model.SharedTheme {
	t := chatThemes[l.scheme]
	s := model.SharedTheme{Scheme: t.id}
	if s.Scheme == "" {
		s.Scheme = model.SchemeDefault
	}
	doodles := !u.bareWall(l)
	solid := func(light, dark color.NRGBA, doodles bool) model.SharedWall {
		return model.SharedWall{Kind: model.WallColor, Light: rgbOf(light), Dark: rgbOf(dark), Doodles: doodles}
	}
	s.Wall = model.SharedWall{Kind: model.WallDefault, Doodles: doodles && t.kind != minimal}
	kind, arg, _ := strings.Cut(l.wall, ":")
	switch kind {
	case "default":
		if l.scheme != 0 {
			s.Wall = solid(lightPalette.ChatBg, darkPalette.ChatBg, doodles)
		}
	case "plain":
		s.Wall = solid(lightPalette.Panel, darkPalette.Panel, false)
	case "theme":
		for _, t := range chatThemes {
			if t.base == arg && t.kind == vibrant {
				s.Wall = solid(rgb(t.light.wall), rgb(t.dark.wall), doodles)
			}
		}
	case "color":
		if i, err := strconv.Atoi(arg); err == nil && i >= 0 && i < len(solidWalls) {
			s.Wall = solid(rgb(solidWalls[i].light), rgb(solidWalls[i].dark), doodles)
		}
	case "solid":
		if w, ok := parseSolidWall(arg); ok {
			s.Wall = solid(rgb(w.light), rgb(w.dark), doodles)
			s.Wall.DoodleLight, s.Wall.DoodleDark = w.doodleLight, w.doodleDark
		}
	case "shared":
		s.Wall = model.SharedWall{Kind: model.WallPhoto, Photo: arg}
	case "photo":
		if file, _ := parsePhotoWall(arg); wallpaperDir != "" && file != "" {
			s.Wall = model.SharedWall{Kind: model.WallPhoto, Photo: filepath.Join(wallpaperDir, filepath.Base(file))}
		}
	}
	return s
}

// rgbOf is a color as 0xRRGGBB.
func rgbOf(c color.NRGBA) uint32 { return uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B) }

// shareRows are the first page's rows for everyone in chat id, where its
// theme can be set for everyone.
func (u *UI) shareRows(gtx C, id string) []layout.Widget {
	c := u.chatByID(id)
	if id == lookAll || c == nil || c.Left || isChannelID(id) {
		return nil
	}
	if u.btn("theme:share").Clicked(gtx) && u.mayShareTheme(c) {
		u.confirm("Set this theme for everyone?", "Everyone in this chat will see this theme and wallpaper. "+
			"Each of them can still change it for themselves.",
			dialogButton{label: "Set for everyone", primary: true, run: func() { u.shareTheme(id) }})
	}
	if u.btn("theme:unshare").Clicked(gtx) && u.mayShareTheme(c) {
		u.confirm("Reset the theme for everyone?", "This chat will go back to the theme each person has for their chats.",
			dialogButton{label: "Reset for everyone", primary: true, run: func() {
				u.resetLook(id)
				u.backend.ShareTheme(id, model.SharedTheme{Reset: true})
			}})
	}
	p := u.pal
	none := func(gtx C) D { return D{} }
	return []layout.Widget{
		func(gtx C) D {
			sz := image.Pt(gtx.Constraints.Max.X, gtx.Dp(41))
			fillRect(gtx, image.Rect(0, gtx.Dp(20), sz.X, gtx.Dp(20)+max(1, gtx.Dp(1))), p.Divider)
			return D{Size: sz}
		},
		func(gtx C) D {
			return layout.Inset{Top: 4, Bottom: 8}.Layout(gtx, u.label(16.5, "Everyone", p.Text, labelOpts{weight: font.Medium}).Layout)
		},
		func(gtx C) D { return u.themeRow(gtx, "theme:share", icGroupsLine, "Set for everyone", none) },
		func(gtx C) D { return u.themeRow(gtx, "theme:unshare", icRefresh, "Reset for everyone", none) },
	}
}

// mayShareTheme reports whether you may set c's theme for everyone, saying
// why not when you may not. It's one of a group's settings: any member may,
// unless only admins edit those.
func (u *UI) mayShareTheme(c *model.Chat) bool {
	if !c.IsGroup {
		return true
	}
	info := u.chatMembers(c.ID)
	if info == nil || !info.Locked || len(info.Members) == 0 || u.isAdminIn(info) {
		return true
	}
	u.inform("Only admins can change this", "Only group admins can set the theme for everyone in this group.")
	return false
}

// shareTheme sets chat id's look for everyone, and keeps it as yours.
func (u *UI) shareTheme(id string) {
	u.setLook(id, u.lookOf(id))
	u.backend.ShareTheme(id, u.shareable(u.lookOf(id)))
}
