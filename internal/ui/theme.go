package ui

import (
	"image/color"

	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/text"
	"gioui.org/widget/material"
)

// Palette holds every color the UI uses. Values follow WhatsApp Desktop.
type Palette struct {
	Rail, RailBorder, RailIcon, RailActive, RailActiveBg color.NRGBA

	Panel, Divider, Title    color.NRGBA
	Search, SearchHint       color.NRGBA
	Chip, ChipText           color.NRGBA
	ChipActive, ChipActiveFg color.NRGBA
	RowHover, RowSelected    color.NRGBA

	Text, TextSecondary, Icon color.NRGBA
	Green, Badge, BadgeText   color.NRGBA

	Header                        color.NRGBA
	ChatBg, Doodle                color.NRGBA
	BubbleIn, BubbleOut, Shadow   color.NRGBA
	Meta, TickRead                color.NRGBA
	QuoteIn, QuoteOut             color.NRGBA
	SystemChip, SystemChipText    color.NRGBA
	Encryption, EncryptionText    color.NRGBA
	Composer, Input, InputHint    color.NRGBA
	Hover                         color.NRGBA
	EmptyBg, EmptyText, EmptyLine color.NRGBA
}

func rgb(c uint32) color.NRGBA {
	return color.NRGBA{R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c), A: 0xff}
}

func argb(c uint32, a uint8) color.NRGBA {
	col := rgb(c)
	col.A = a
	return col
}

var lightPalette = Palette{
	Rail: rgb(0xf7f5f3), RailBorder: rgb(0xe9edef), RailIcon: rgb(0x54656f),
	RailActive: rgb(0x111b21), RailActiveBg: argb(0x0b141a, 0x1a),

	Panel: rgb(0xffffff), Divider: rgb(0xe9edef), Title: rgb(0x1daa61),
	Search: rgb(0xf6f5f4), SearchHint: rgb(0x667781),
	Chip: rgb(0xf6f5f4), ChipText: rgb(0x54656f),
	ChipActive: rgb(0xd9fdd3), ChipActiveFg: rgb(0x15603e),
	RowHover: rgb(0xf6f5f4), RowSelected: rgb(0xf0f2f5),

	Text: rgb(0x111b21), TextSecondary: rgb(0x667781), Icon: rgb(0x54656f),
	Green: rgb(0x1daa61), Badge: rgb(0x25d366), BadgeText: rgb(0xffffff),

	Header: rgb(0xf0f2f5),
	ChatBg: rgb(0xefeae2), Doodle: rgb(0xe7e1d8),
	BubbleIn: rgb(0xffffff), BubbleOut: rgb(0xd9fdd3), Shadow: argb(0x0b141a, 0x21),
	Meta: rgb(0x667781), TickRead: rgb(0x53bdeb),
	QuoteIn: rgb(0xf5f6f6), QuoteOut: rgb(0xd1f4cc),
	SystemChip: rgb(0xffffff), SystemChipText: rgb(0x54656f),
	Encryption: rgb(0xffeecd), EncryptionText: rgb(0x54656f),
	Composer: rgb(0xf0f2f5), Input: rgb(0xffffff), InputHint: rgb(0x667781),
	Hover:   argb(0x0b141a, 0x10),
	EmptyBg: rgb(0xf0f2f5), EmptyText: rgb(0x667781), EmptyLine: rgb(0x25d366),
}

var darkPalette = Palette{
	Rail: rgb(0x202c33), RailBorder: rgb(0x2a3942), RailIcon: rgb(0xaebac1),
	RailActive: rgb(0xe9edef), RailActiveBg: argb(0xffffff, 0x1a),

	Panel: rgb(0x111b21), Divider: rgb(0x222d34), Title: rgb(0xe9edef),
	Search: rgb(0x202c33), SearchHint: rgb(0x8696a0),
	Chip: rgb(0x202c33), ChipText: rgb(0x8696a0),
	ChipActive: rgb(0x0a332c), ChipActiveFg: rgb(0x00a884),
	RowHover: rgb(0x202c33), RowSelected: rgb(0x2a3942),

	Text: rgb(0xe9edef), TextSecondary: rgb(0x8696a0), Icon: rgb(0xaebac1),
	Green: rgb(0x00a884), Badge: rgb(0x00a884), BadgeText: rgb(0x111b21),

	Header: rgb(0x202c33),
	ChatBg: rgb(0x0b141a), Doodle: rgb(0x142027),
	BubbleIn: rgb(0x202c33), BubbleOut: rgb(0x005c4b), Shadow: argb(0x000000, 0x30),
	Meta: argb(0xffffff, 0x99), TickRead: rgb(0x53bdeb),
	QuoteIn: rgb(0x1d282f), QuoteOut: rgb(0x025144),
	SystemChip: rgb(0x182229), SystemChipText: rgb(0x8696a0),
	Encryption: rgb(0x182229), EncryptionText: rgb(0xffd279),
	Composer: rgb(0x202c33), Input: rgb(0x2a3942), InputHint: rgb(0x8696a0),
	Hover:   argb(0xffffff, 0x10),
	EmptyBg: rgb(0x222e35), EmptyText: rgb(0x8696a0), EmptyLine: rgb(0x00a884),
}

// senderColors tint group sender names, like WhatsApp does.
var senderColors = []uint32{
	0x1f7aec, 0xe542a3, 0x02a698, 0xc85a00, 0x7f66ff, 0xd62f45, 0x029d00, 0x0e8a94, 0xa4661f,
}

// avatarColors are backgrounds for initials avatars.
var avatarColors = []uint32{
	0x6bcbef, 0xffbc38, 0xe542a3, 0x91ab01, 0x35cd96, 0x7f66ff, 0xfe7c7f, 0x53a6fd, 0xba5ae8,
}

func hashIndex(s string, n int) int {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return int(h % uint32(n))
}

// Typeface prefers Segoe UI (what WhatsApp Desktop uses on Windows), then
// common system UI fonts, then the bundled Go fonts.
const typeface font.Typeface = "Segoe UI, Helvetica Neue, Roboto, Noto Sans, sans-serif, Go"

func newTheme() *material.Theme {
	th := material.NewTheme()
	// System fonts are enabled by default; gofont is the last-resort fallback.
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	th.Face = typeface
	th.TextSize = 14
	return th
}
