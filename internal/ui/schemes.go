// The palettes' colors (scheme.go), from each one's own source:
//
//	Catppuccin   github.com/catppuccin/palette (palette.json), MIT
//	Tokyo Night  github.com/folke/tokyonight.nvim, Apache-2.0
//	Gruvbox      github.com/ellisonleao/gruvbox.nvim (morhetz's colors), MIT
//	Everforest   github.com/sainnhe/everforest (medium contrast; hard's bg_dim), MIT
//	Solarized    github.com/altercation/solarized, MIT
//	Ayu          github.com/ayu-theme/ayu-colors (themes/dark.yaml, light.yaml), MIT
//
// Their licenses are the LICENSE-<name> files here. A color marked derived
// isn't the scheme's own: Solarized has two backgrounds a theme and Ayu
// gives its lines and text as a color with an alpha, mixed here.

package ui

import "strings"

// schemes are the palettes, in the order Settings lists them.
var schemes = []scheme{
	{id: "catppuccin", name: "Catppuccin", sub: "Mocha, and Latte in the light theme", light: &catLatte, dark: &catMocha, accent: "purple"},
	{id: "catppuccin-macchiato", name: "Catppuccin Macchiato", sub: "Latte in the light theme", light: &catLatte, dark: &catMacchiato, accent: "purple"},
	{id: "catppuccin-frappe", name: "Catppuccin Frappé", sub: "Latte in the light theme", light: &catLatte, dark: &catFrappe, accent: "purple"},
	{id: "tokyonight", name: "Tokyo Night", sub: "Night, and Day in the light theme", light: &tokyoDay, dark: &tokyoNight, accent: "blue"},
	{id: "tokyonight-storm", name: "Tokyo Night Storm", sub: "Day in the light theme", light: &tokyoDay, dark: &tokyoStorm, accent: "blue"},
	{id: "tokyonight-moon", name: "Tokyo Night Moon", sub: "Day in the light theme", light: &tokyoDay, dark: &tokyoMoon, accent: "blue"},
	{id: "gruvbox", name: "Gruvbox", sub: "Warm browns, and cream in the light theme", light: &gruvboxLight, dark: &gruvboxDark, accent: "orange"},
	{id: "everforest", name: "Everforest", sub: "Soft greens", light: &everforestLight, dark: &everforestDark, accent: "green"},
	{id: "solarized", name: "Solarized", sub: "Deep teal, and cream in the light theme", light: &solarizedLight, dark: &solarizedDark, accent: "cyan"},
	{id: "ayu", name: "Ayu", sub: "Near black and gold, and white in the light theme", light: &ayuLight, dark: &ayuDark, accent: "gold"},
}

// hues pairs a flavor's accents with their ids (space separated).
func hues(ids string, c ...uint32) []hue {
	hs := make([]hue, len(c))
	for i, id := range strings.Fields(ids) {
		hs[i] = hue{id, c[i]}
	}
	return hs
}

// catHues are a Catppuccin flavor's accents in its own order: rosewater,
// flamingo, pink, mauve, red, maroon, peach, yellow, green, teal, sky,
// sapphire, blue and lavender.
func catHues(c ...uint32) []hue {
	return hues("rosewater flamingo pink purple red maroon orange yellow green teal cyan sapphire blue lavender", c...)
}

// tokyoHues are a Tokyo Night flavor's: red, orange, yellow, green, green1,
// teal, cyan, blue1, blue, magenta and purple.
func tokyoHues(c ...uint32) []hue {
	return hues("red orange yellow green aqua teal cyan sapphire blue purple pink", c...)
}

// Gruvbox's and Everforest's hues: red, orange, yellow, green, aqua, blue
// and purple. Solarized's: yellow, orange, red, magenta, violet, blue, cyan
// and green. Ayu's: red, pink, orange, peach, yellow, green, teal, indigo,
// blue, purple and its accent.
const (
	warmHues  = "red orange yellow green teal blue purple"
	solarHues = "yellow orange red pink purple blue cyan green"
	ayuHues   = "red flamingo orange sand yellow green teal cyan blue purple gold"
)

var (
	// Catppuccin: crust, mantle, base; surface0 and 1; overlay1, subtext0, text.
	catLatte = flavor{crust: 0xdce0e8, mantle: 0xe6e9ef, base: 0xeff1f5, surface0: 0xccd0da, surface1: 0xbcc0cc,
		overlay: 0x8c8fa1, subtext: 0x6c6f85, text: 0x4c4f69,
		hues: catHues(0xdc8a78, 0xdd7878, 0xea76cb, 0x8839ef, 0xd20f39, 0xe64553, 0xfe640b, 0xdf8e1d, 0x40a02b, 0x179299,
			0x04a5e5, 0x209fb5, 0x1e66f5, 0x7287fd)}
	catFrappe = flavor{crust: 0x232634, mantle: 0x292c3c, base: 0x303446, surface0: 0x414559, surface1: 0x51576d,
		overlay: 0x838ba7, subtext: 0xa5adce, text: 0xc6d0f5,
		hues: catHues(0xf2d5cf, 0xeebebe, 0xf4b8e4, 0xca9ee6, 0xe78284, 0xea999c, 0xef9f76, 0xe5c890, 0xa6d189, 0x81c8be,
			0x99d1db, 0x85c1dc, 0x8caaee, 0xbabbf1)}
	catMacchiato = flavor{crust: 0x181926, mantle: 0x1e2030, base: 0x24273a, surface0: 0x363a4f, surface1: 0x494d64,
		overlay: 0x8087a2, subtext: 0xa5adcb, text: 0xcad3f5,
		hues: catHues(0xf4dbd6, 0xf0c6c6, 0xf5bde6, 0xc6a0f6, 0xed8796, 0xee99a0, 0xf5a97f, 0xeed49f, 0xa6da95, 0x8bd5ca,
			0x91d7e3, 0x7dc4e4, 0x8aadf4, 0xb7bdf8)}
	catMocha = flavor{crust: 0x11111b, mantle: 0x181825, base: 0x1e1e2e, surface0: 0x313244, surface1: 0x45475a,
		overlay: 0x7f849c, subtext: 0xa6adc8, text: 0xcdd6f4,
		hues: catHues(0xf5e0dc, 0xf2cdcd, 0xf5c2e7, 0xcba6f7, 0xf38ba8, 0xeba0ac, 0xfab387, 0xf9e2af, 0xa6e3a1, 0x94e2d5,
			0x89dceb, 0x74c7ec, 0x89b4fa, 0xb4befe)}

	// Tokyo Night: bg_dark1, bg_dark, bg; bg_highlight and fg_gutter (Storm,
	// whose bg_highlight is nearly its bg: fg_gutter and terminal_black);
	// dark5 (Day: dark3), fg_dark, fg.
	tokyoNight = flavor{crust: 0x0c0e14, mantle: 0x16161e, base: 0x1a1b26, surface0: 0x292e42, surface1: 0x3b4261,
		overlay: 0x737aa2, subtext: 0xa9b1d6, text: 0xc0caf5,
		hues: tokyoHues(0xf7768e, 0xff9e64, 0xe0af68, 0x9ece6a, 0x73daca, 0x1abc9c, 0x7dcfff, 0x2ac3de, 0x7aa2f7, 0xbb9af7, 0x9d7cd8)}
	tokyoStorm = flavor{crust: 0x1b1e2d, mantle: 0x1f2335, base: 0x24283b, surface0: 0x3b4261, surface1: 0x414868,
		overlay: 0x737aa2, subtext: 0xa9b1d6, text: 0xc0caf5,
		hues: tokyoHues(0xf7768e, 0xff9e64, 0xe0af68, 0x9ece6a, 0x73daca, 0x1abc9c, 0x7dcfff, 0x2ac3de, 0x7aa2f7, 0xbb9af7, 0x9d7cd8)}
	tokyoMoon = flavor{crust: 0x191b29, mantle: 0x1e2030, base: 0x222436, surface0: 0x2f334d, surface1: 0x3b4261,
		overlay: 0x737aa2, subtext: 0x828bb8, text: 0xc8d3f5,
		hues: tokyoHues(0xff757f, 0xff966c, 0xffc777, 0xc3e88d, 0x41a6b5, 0x4fd6be, 0x86e1fc, 0x65bcff, 0x82aaff, 0xc099ff, 0xfca7ea)}
	tokyoDay = flavor{crust: 0xc1c9df, mantle: 0xd0d5e3, base: 0xe1e2e7, surface0: 0xc4c8da, surface1: 0xa8aecb,
		overlay: 0x8990b3, subtext: 0x6172b0, text: 0x3760bf,
		hues: tokyoHues(0xf52a65, 0xb15c00, 0x8c6c3e, 0x587539, 0x387068, 0x118c74, 0x007197, 0x188092, 0x2e7de9, 0x9854f1, 0x7847bd)}

	// Gruvbox: dark0_hard, dark0 (the panels and the chat), dark1 and 2;
	// gray, light4, light1, with the bright hues. Light: light0_soft, light0,
	// light1; light2 and 3; gray, dark4, dark1, with the faded hues.
	gruvboxDark = flavor{crust: 0x1d2021, mantle: 0x282828, base: 0x282828, surface0: 0x3c3836, surface1: 0x504945,
		overlay: 0x928374, subtext: 0xa89984, text: 0xebdbb2,
		hues: hues(warmHues, 0xfb4934, 0xfe8019, 0xfabd2f, 0xb8bb26, 0x8ec07c, 0x83a598, 0xd3869b)}
	gruvboxLight = flavor{crust: 0xebdbb2, mantle: 0xf2e5bc, base: 0xfbf1c7, surface0: 0xd5c4a1, surface1: 0xbdae93,
		overlay: 0x928374, subtext: 0x7c6f64, text: 0x3c3836,
		hues: hues(warmHues, 0x9d0006, 0xaf3a03, 0xb57614, 0x79740e, 0x427b58, 0x076678, 0x8f3f71)}

	// Everforest: bg_dim of the hard and of the medium contrast, bg0; bg2
	// and bg4; grey1, grey2, fg. Light: bg3, bg1, bg0; bg4 and bg5; grey1,
	// statusline2 (grey2 is too pale to read), fg.
	everforestDark = flavor{crust: 0x1e2326, mantle: 0x232a2e, base: 0x2d353b, surface0: 0x3d484d, surface1: 0x4f585e,
		overlay: 0x859289, subtext: 0x9da9a0, text: 0xd3c6aa,
		hues: hues(warmHues, 0xe67e80, 0xe69875, 0xdbbc7f, 0xa7c080, 0x83c092, 0x7fbbb3, 0xd699b6)}
	everforestLight = flavor{crust: 0xe6e2cc, mantle: 0xf4f0d9, base: 0xfdf6e3, surface0: 0xe0dcc7, surface1: 0xbdc3af,
		overlay: 0x939f91, subtext: 0x708089, text: 0x5c6a72,
		hues: hues(warmHues, 0xf85552, 0xf57d26, 0xdfa000, 0x8da101, 0x35a77c, 0x3a94c5, 0xdf69ba)}

	// Solarized: base03 (the panels and the chat) and base02; base01, base0
	// and base2 as text. Light: base2 and base3; base1, base00 and base02.
	// The frame, the light chat and the light surfaces are derived.
	solarizedDark = flavor{crust: 0x00212b, mantle: 0x002b36, base: 0x002b36, surface0: 0x073642, surface1: 0x234a54,
		overlay: 0x586e75, subtext: 0x839496, text: 0xeee8d5,
		hues: hues(solarHues, 0xb58900, 0xcb4b16, 0xdc322f, 0xd33682, 0x6c71c4, 0x268bd2, 0x2aa198, 0x859900)}
	solarizedLight = flavor{crust: 0xe8e1cc, mantle: 0xeee8d5, base: 0xfdf6e3, surface0: 0xddd6c1, surface1: 0xc9c4b0,
		overlay: 0x93a1a1, subtext: 0x657b83, text: 0x073642,
		hues: hues(solarHues, 0xb58900, 0xcb4b16, 0xdc322f, 0xd33682, 0x6c71c4, 0x268bd2, 0x2aa198, 0x859900)}

	// Ayu: surface.base (the panels and the chat) under a derived frame;
	// ui.selection at 25% and 50%; palette.gray, ui.fg and editor.fg, mixed.
	// Light: ui.line mixed (the chat), surface.sunk, surface.lift; ui.line
	// stronger; palette.gray, ui.fg and editor.fg, mixed.
	ayuDark = flavor{crust: 0x0a0c11, mantle: 0x0d1017, base: 0x0d1017, surface0: 0x1c212b, surface1: 0x2a313e,
		overlay: 0x5a6673, subtext: 0x5a6378, text: 0xbfbdb6,
		hues: hues(ayuHues, 0xf07178, 0xf29668, 0xff8f40, 0xe6c08a, 0xffb454, 0xaad94c, 0x95e6cb, 0x39bae6, 0x59c2ff, 0xd2a6ff, 0xe6b450)}
	ayuLight = flavor{crust: 0xe7eaed, mantle: 0xebeef0, base: 0xfcfcfc, surface0: 0xd9dee2, surface1: 0xc7ced5,
		overlay: 0xadaeb1, subtext: 0x828e9f, text: 0x5c6166,
		hues: hues(ayuHues, 0xf07171, 0xf2a191, 0xfa8532, 0xe59645, 0xeba400, 0x86b300, 0x4cbf99, 0x55b4d4, 0x22a4e6, 0xa37acc, 0xf29718)}
)
