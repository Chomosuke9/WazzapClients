// Palettes (Settings > Appearance > App theme > Palette): color schemes from
// elsewhere (Catppuccin, Tokyo Night, Gruvbox...) instead of WhatsApp's own
// colors. Each has a flavor for the light and one for the dark theme, so
// Dark mode goes on working, and the Color grid offers the flavor's own
// accents. A flavor is a few backgrounds, text colors and hues; schemePalette
// works a whole Palette out of them. The tables are in schemes.go.

package ui

import (
	"image/color"
	"math"
)

const (
	prefScheme       = "scheme"        // a scheme's id, or "" for WhatsApp's colors
	prefSchemeAccent = "scheme_accent" // a hue's id, or "" for the scheme's own accent
)

// scheme is a palette: a flavor for each theme and the hue it accents with
// until another is picked.
type scheme struct {
	id, name, sub string
	light, dark   *flavor
	accent        string
}

// flavor is one variant of a scheme, as 0xRRGGBB. The backgrounds go from
// crust (the frame) through mantle (panels) to base (the chat), and the
// surfaces lie on them (bubbles, hovered rows, lines), named as Catppuccin
// names them.
type flavor struct {
	crust, mantle, base     uint32
	surface0, surface1      uint32
	overlay, subtext, text  uint32
	hues                    []hue
	palettes                map[string]*Palette // by accent, so u.pal stays the same pointer
	members, senders, tints []color.NRGBA
}

// hue is one of a flavor's accents. The ids are the same in every scheme
// where they can be (red, blue...), so a picked accent survives a change of
// scheme.
type hue struct {
	id string
	c  uint32
}

// schemeOf returns the scheme with an id, or nil for WhatsApp's colors.
func schemeOf(id string) *scheme {
	for i := range schemes {
		if schemes[i].id == id {
			return &schemes[i]
		}
	}
	return nil
}

// flavor returns the scheme's flavor for a theme.
func (s *scheme) flavor(dark bool) *flavor {
	if dark {
		return s.dark
	}
	return s.light
}

// hue returns the first of ids the flavor has.
func (f *flavor) hue(ids ...string) (color.NRGBA, bool) {
	for _, id := range ids {
		for _, h := range f.hues {
			if h.id == id {
				return rgb(h.c), true
			}
		}
	}
	return color.NRGBA{}, false
}

// appPalette is the app's palette: WhatsApp's with its App theme color, or
// a scheme's flavor with one of its hues.
func appPalette(dark bool, schemeID, accent, schemeAccent string) *Palette {
	s := schemeOf(schemeID)
	if s == nil {
		if dark {
			return withAccent(&darkPalette, true, accent)
		}
		return withAccent(&lightPalette, false, accent)
	}
	f := s.flavor(dark)
	if _, ok := f.hue(schemeAccent); !ok {
		schemeAccent = s.accent
	}
	if p := f.palettes[schemeAccent]; p != nil {
		return p
	}
	p := schemePalette(f, dark, schemeAccent)
	if f.palettes == nil {
		f.palettes = map[string]*Palette{}
	}
	f.palettes[schemeAccent] = p
	return p
}

// schemePalette builds a flavor's palette. What a flavor has no say in (the
// shadows, the status viewer's backdrop, the QR code) stays WhatsApp's.
func schemePalette(f *flavor, dark bool, accent string) *Palette {
	q := lightPalette
	if dark {
		q = darkPalette
	}
	crust, mantle, base := rgb(f.crust), rgb(f.mantle), rgb(f.base)
	s0, s1 := rgb(f.surface0), rgb(f.surface1)
	overlay, sub, text := rgb(f.overlay), rgb(f.subtext), rgb(f.text)
	acc, _ := f.hue(accent)
	// pick is a hue by what it's for; a flavor without it gets the accent.
	pick := func(ids ...string) color.NRGBA {
		if c, ok := f.hue(ids...); ok {
			return c
		}
		return acc
	}
	red, yellow, orange := pick("red"), pick("yellow"), pick("orange")
	blue, purple := pick("blue"), pick("purple")

	// A dark flavor's panels are darker than the chat and what lies on them
	// lighter. A light one's are its lightest color, as its bubbles are,
	// on a chat a little darker, like WhatsApp's.
	frame, panel, chat, raised, hover, lift := crust, mantle, base, s0, s0, s1
	if !dark {
		frame, panel, chat, raised, hover, lift = mantle, base, crust, base, mantle, mantle
	}
	q.Frame, q.FrameText = frame, text
	q.PanelBorder, q.Divider = s0, s0
	q.Panel, q.ChatBg = panel, chat
	q.Hover, q.Selected, q.Search, q.RowHover = hover, hover, hover, hover
	q.Chip, q.ChipBorder, q.ChipText = panel, s1, sub
	pale := mix(panel, acc, 0.22)
	q.ChipActive, q.ChipActiveBorder, q.ChipActiveText = pale, pale, acc
	q.Text, q.TextSecondary, q.Icon, q.IconActive, q.IconStrong = text, sub, sub, text, text
	// The hues are mostly pastels on a dark flavor and deep on a light one, so
	// what lies on the accent is the flavor's darkest or lightest background.
	q.Green, q.OnGreen = acc, crust
	if !dark {
		q.OnGreen = base
	}
	// A bright accent on a light flavor (Ayu's, Everforest's) takes the
	// text's color, darkened when that doesn't read either (3:1).
	if contrast(text, acc) > contrast(q.OnGreen, acc) {
		q.OnGreen = text
	}
	if contrast(q.OnGreen, acc) < 3 {
		q.OnGreen = mix(text, rgb(0x000000), 0.6)
	}
	q.RailActive, q.RailSeparator = s0, s0

	q.Doodle, q.Plain = mix(chat, s0, 0.5), false
	// Your bubble is tinted with the accent, as far as the text still reads
	// on it (4.5:1) and at least enough to tell it from the others' (Tokyo
	// Night Day, whose text is blue, stops there)..
	tint := float32(0.3)
	for tint > 0.18 && contrast(text, mix(raised, acc, tint)) < 4.5 {
		tint -= 0.02
	}
	if dark {
		q.BubbleOut = mix(chat, acc, tint)
	} else {
		q.BubbleOut = mix(raised, acc, tint)
	}
	q.BubbleIn = raised
	q.MetaIn, q.MetaOut, q.SecondaryOut, q.TextOut = sub, sub, sub, text
	q.TickRead, q.Link, q.BubbleButton = blue, blue, blue
	q.LinkOut, q.TickReadOut = color.NRGBA{}, color.NRGBA{}
	q.QuoteIn, q.QuoteOut = mix(raised, crust, 0.35), mix(q.BubbleOut, crust, 0.3)
	q.DateChip, q.DateChipText = frame, sub
	q.Encryption, q.EncryptionText = frame, yellow
	q.Composer, q.ComposerHint = raised, sub
	q.GroupAvatar, q.GroupAvatarIcon = mix(panel, blue, 0.25), blue
	q.UserAvatar, q.UserAvatarIcon = s1, sub
	q.Banner, q.BannerText = s0, text
	q.Menu, q.MenuHover = raised, lift
	q.EmptyIcon = s1
	q.Danger, q.DangerSoft = red, mix(red, text, 0.3)
	q.RingViewed, q.RingAudience, q.Verified = overlay, purple, blue
	q.AnnounceBg, q.AnnounceIcon = mix(panel, yellow, 0.2), yellow
	q.ChannelAvatar, q.ChannelAvatarIcon = mix(panel, orange, 0.2), orange

	q.Popup, q.PopupBorder, q.PopupHover = panel, s1, hover
	q.PopupDivider, q.PopupSub = s0, sub
	q.Picker, q.PickerTab, q.PickerTabBorder = panel, hover, s1
	q.Viewer, q.ViewerDivider, q.ViewerThumb, q.ViewerArrow = frame, s0, s0, s0
	q.Dialog = base
	q.Toast, q.ToastText = text, base
	q.MentionPill = mix(q.BubbleIn, acc, 0.25)
	q.Selection = mix(base, blue, 0.35)

	if !dark {
		q.UserAvatar = s0
		q.QuoteIn, q.QuoteOut = mantle, mix(q.BubbleOut, crust, 0.5)
		q.DateChip = base
		q.ChipActiveText = mix(acc, text, 0.4)
		// A deep yellow doesn't read on grey.
		q.Encryption, q.EncryptionText = mix(base, yellow, 0.18), mix(yellow, text, 0.6)
	}

	if f.members == nil {
		f.members = make([]color.NRGBA, len(q.Members))
		for i := range f.members {
			// Once the hues run out they come again, nearer the text's color.
			c := rgb(f.hues[i%len(f.hues)].c)
			if i >= len(f.hues) {
				c = mix(c, text, 0.4)
			}
			f.members[i] = c
		}
		// WhatsApp's order (pink, sky, sand, violet, teal, orange, blue,
		// brown, red, green), which personTint indexes.
		f.senders = []color.NRGBA{pick("pink", "purple"), pick("cyan", "teal", "blue"), yellow, purple,
			pick("teal", "cyan", "green"), orange, blue, pick("flamingo", "sapphire", "orange"), red, pick("green")}
		f.tints = make([]color.NRGBA, len(f.senders))
		for i, c := range f.senders {
			f.tints[i] = mix(panel, c, 0.25)
		}
	}
	q.Members, q.Senders, q.AvatarBgs = f.members, f.senders, f.tints
	return &q
}

// contrast is the contrast ratio of two colors (WCAG's, 1 to 21).
func contrast(a, b color.NRGBA) float64 {
	lum := func(c color.NRGBA) float64 {
		lin := func(v uint8) float64 {
			x := float64(v) / 255
			if x <= 0.04045 {
				return x / 12.92
			}
			return math.Pow((x+0.055)/1.055, 2.4)
		}
		return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
	}
	la, lb := lum(a), lum(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// setScheme picks the palette.
func (u *UI) setScheme(id string) {
	u.backend.SetPref(prefScheme, id)
	u.applyTheme()
}

// setSchemeAccent picks one of the palette's hues as the accent.
func (u *UI) setSchemeAccent(s *scheme, id string) {
	if id == s.accent {
		id = ""
	}
	u.backend.SetPref(prefSchemeAccent, id)
	u.applyTheme()
}

// SetScheme draws with a palette and one of its hues ("" is its own), as
// Settings would set them (used for screenshots).
func (u *UI) SetScheme(id, accent string) {
	u.scheme, u.schemeAccent = id, accent
	u.SetDark(u.dark)
}

// schemeRows are the App theme page's Palette choices.
func (u *UI) schemeRows() []settingRow {
	rows := []settingRow{{key: "scheme:", kind: setRadio, title: "WhatsApp", on: schemeOf(u.scheme) == nil,
		run: func() { u.setScheme("") }}}
	for i := range schemes {
		s := &schemes[i]
		rows = append(rows, settingRow{key: "scheme:" + s.id, kind: setRadio, title: s.name, sub: s.sub,
			on: u.scheme == s.id, run: func() { u.setScheme(s.id) }})
	}
	return rows
}
