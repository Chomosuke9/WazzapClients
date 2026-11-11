package ui

import (
	"image"

	"gioui.org/layout"
)

// App theme: the app's accent color, as the Android app's Settings >
// Appearance > App theme picks it (a WhatsApp Plus feature there). Each color is a
// chat theme's (accentColors, in chatthemes.go): its swatch is the theme's
// vibrant bubble and the accent its tonal scheme's, which then colors
// buttons, badges, the active filter chip, links and read ticks. WhatsApp's
// green, the first, is the palette as it is.

// prefAccent is the App theme color: a theme's ID, "" for WhatsApp's green
// (an app preference, appPrefs).
const prefAccent = "accent"

type accentColor struct {
	base        string
	light, dark accentSet
}

// accentSet is an App theme color in one mode, as 0xRRGGBB.
type accentSet struct {
	swatch               uint32
	accent, pale, strong uint32 // the accent, its deemphasized and emphasized variants
	link                 uint32
}

// accentPals keeps each accented palette, so u.pal stays the same pointer
// from frame to frame.
var accentPals = map[accentKey]*Palette{}

type accentKey struct {
	base *Palette
	id   string
}

// withAccent returns palette p with App theme color id.
func withAccent(p *Palette, dark bool, id string) *Palette {
	if id == "" || id == accentColors[0].base {
		return p
	}
	k := accentKey{p, id}
	if q, ok := accentPals[k]; ok {
		return q
	}
	for _, a := range accentColors {
		if a.base != id {
			continue
		}
		s := a.light
		if dark {
			s = a.dark
		}
		q := *p
		q.Green = rgb(s.accent)
		q.ChipActive, q.ChipActiveBorder, q.ChipActiveText = rgb(s.pale), rgb(s.pale), rgb(s.strong)
		q.Link, q.LinkOut = rgb(s.link), rgb(s.link)
		q.TickRead, q.TickReadOut = rgb(s.accent), rgb(s.accent)
		q.MentionPill = argb(s.accent, 0x24)
		accentPals[k] = &q
		return &q
	}
	return p
}

// setAccent picks the App theme color.
func (u *UI) setAccent(id string) {
	if id == accentColors[0].base {
		id = ""
	}
	u.backend.SetPref(prefAccent, id)
	u.accent = id
	u.SetDark(u.dark)
}

// accentRows is the App theme page's Color grid.
func (u *UI) accentRows() layout.Widget {
	return func(gtx C) D {
		if s := schemeOf(u.scheme); s != nil {
			return u.hueRows(gtx, s)
		}
		cur := u.accent
		if cur == "" {
			cur = accentColors[0].base
		}
		cell, gap := image.Pt(gtx.Dp(72), gtx.Dp(72)), gtx.Dp(12)
		w := gtx.Constraints.Max.X - gtx.Dp(32)
		rows := grid(len(accentColors), cell, gap, w, func(gtx C, i int) {
			a := accentColors[i]
			s := a.light
			if u.dark {
				s = a.dark
			}
			if _, clicked := u.circle(gtx, "accent:"+a.base, rgb(s.swatch), u.pal.Panel, a.base == cur); clicked {
				u.setAccent(a.base)
			}
		})
		children := make([]layout.FlexChild, len(rows))
		for i, r := range rows {
			children[i] = layout.Rigid(r)
		}
		return layout.Inset{Left: 16, Right: 16, Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	}
}

// hueRows is the Color grid under a palette: its flavor's own hues.
func (u *UI) hueRows(gtx C, s *scheme) D {
	f := s.flavor(u.dark)
	cur := u.schemeAccent
	if _, ok := f.hue(cur); !ok {
		cur = s.accent
	}
	cell, gap := image.Pt(gtx.Dp(72), gtx.Dp(72)), gtx.Dp(12)
	w := gtx.Constraints.Max.X - gtx.Dp(32)
	rows := grid(len(f.hues), cell, gap, w, func(gtx C, i int) {
		h := f.hues[i]
		if _, clicked := u.circle(gtx, "hue:"+h.id, rgb(h.c), u.pal.Panel, h.id == cur); clicked {
			u.setSchemeAccent(s, h.id)
		}
	})
	children := make([]layout.FlexChild, len(rows))
	for i, r := range rows {
		children[i] = layout.Rigid(r)
	}
	return layout.Inset{Left: 16, Right: 16, Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}
