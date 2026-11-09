package ui

import (
	"hash/fnv"
	"image/color"
	"strings"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/text"
	"gioui.org/widget/material"
)

// Palette holds every color the UI uses. Most come from the design tokens of
// WhatsApp Web and Desktop (WDSLightTheme and WDSDarkTheme in WDSThemes, the
// --WDS-* variables of web.whatsapp.com's CSS). Gio blends in linear space, so
// a token with alpha (content-deemphasized is black or white at 60%) is
// pre-mixed here, in sRGB, over the surface it's usually drawn on. The rest
// (the frame's borders, wallpaper doodles, viewer, menus' round buttons) were
// sampled from WhatsApp Desktop screenshots.
type Palette struct {
	Frame, FrameText                             color.NRGBA // title bar and rail
	PanelBorder, Divider                         color.NRGBA
	Panel, ChatBg                                color.NRGBA
	Hover, Selected, Search                      color.NRGBA
	Chip, ChipBorder, ChipText                   color.NRGBA
	ChipActive, ChipActiveBorder, ChipActiveText color.NRGBA

	Text, TextSecondary, Icon, IconActive color.NRGBA
	IconStrong                            color.NRGBA // header, composer and menu buttons
	Green, OnGreen                        color.NRGBA
	RailActive, RailSeparator             color.NRGBA

	Doodle                    color.NRGBA
	BubbleIn, BubbleOut       color.NRGBA
	MetaIn, MetaOut, TickRead color.NRGBA
	Link                      color.NRGBA // web links and mentions in messages
	TextOut, SecondaryOut     color.NRGBA
	// LinkOut and TickReadOut are links and read ticks in your bubbles,
	// white in a vibrant chat theme; zero means Link and TickRead.
	LinkOut, TickReadOut color.NRGBA
	// Plain is a wallpaper without doodles (a minimal chat theme), and
	// WallPhoto a photo chosen as the chat's wallpaper (chattheme.go),
	// cropped around WallFocus (wallphoto.go).
	Plain                        bool
	WallPhoto                    string
	WallFocus                    f32.Point
	QuoteIn, QuoteOut            color.NRGBA
	BubbleLine, BubbleButton     color.NRGBA // dividers and text of message buttons
	CodeBg                       color.NRGBA // behind `inline code`
	MentionPill                  color.NRGBA // behind mentions of you
	Selection                    color.NRGBA // behind selected text
	DateChip, DateChipText       color.NRGBA
	UnreadBand                   color.NRGBA // behind "N unread messages" (unread.go)
	Encryption, EncryptionText   color.NRGBA
	Composer, ComposerHint       color.NRGBA
	GroupAvatar, GroupAvatarIcon color.NRGBA
	UserAvatar, UserAvatarIcon   color.NRGBA
	// AvatarBgs is behind the initial of someone without a picture, which is
	// drawn in their color from Senders; personTint picks both. GeneralAvatar
	// is behind a community's General chat's speech bubble.
	AvatarBgs                []color.NRGBA
	GeneralAvatar            color.NRGBA
	Banner, BannerText, QRFg color.NRGBA
	Menu, MenuHover, Shadow  color.NRGBA
	CloseHover               color.NRGBA
	Senders                  []color.NRGBA
	// Members are the colors of names in a chat, given by memberColors:
	// WhatsApp Web's useWAWebGroupColors, 1 to 17.
	Members []color.NRGBA

	EmptyIcon                        color.NRGBA // big glyphs of empty panes
	RowHover                         color.NRGBA // settings and info list rows
	Danger, DangerSoft               color.NRGBA // "Log out"; "Exit group" and friends
	RingViewed                       color.NRGBA // status ring once seen
	RingAudience                     color.NRGBA // status ring of one shared with close friends or a custom audience
	Verified                         color.NRGBA
	AnnounceBg, AnnounceIcon         color.NRGBA // community announcements tile
	ChannelAvatar, ChannelAvatarIcon color.NRGBA // channel without a picture
	StatusBg                         color.NRGBA // status viewer backdrop

	Popup, PopupBorder, PopupHover color.NRGBA // context menus and the reaction bar
	PopupDivider, PopupSub         color.NRGBA
	Picker, PickerTab              color.NRGBA // emoji picker panel; its active tab
	PickerTabBorder                color.NRGBA
	Viewer, ViewerDivider          color.NRGBA // media viewer
	ViewerThumb, ViewerArrow       color.NRGBA
	Scrim                          color.NRGBA // behind dialogs
	Dialog                         color.NRGBA
	Toast, ToastText               color.NRGBA
}

// markColors are what the photo editor draws with, the same in both
// themes since they end up in the photo.
var markColors = rgbs(0xffffff, 0x000000, 0xff3b30, 0xff9500, 0xffcc00, 0x34c759, 0x007aff, 0xaf52de)

func rgb(c uint32) color.NRGBA {
	return color.NRGBA{R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c), A: 0xff}
}

func argb(c uint32, a uint8) color.NRGBA {
	col := rgb(c)
	col.A = a
	return col
}

func rgbs(cs ...uint32) []color.NRGBA {
	out := make([]color.NRGBA, len(cs))
	for i, c := range cs {
		out[i] = rgb(c)
	}
	return out
}

var darkPalette = Palette{
	Frame: rgb(0x1d1f1f), FrameText: rgb(0xfafafa),
	PanelBorder: rgb(0x2c2c2c), Divider: rgb(0x2d2e2e),
	Panel: rgb(0x161717), ChatBg: rgb(0x161717),
	Hover: rgb(0x2d2e2e), Selected: rgb(0x2d2e2e), Search: rgb(0x2d2e2e),
	Chip: rgb(0x161717), ChipBorder: rgb(0x2d2e2e), ChipText: rgb(0xa2a2a2),
	ChipActive: rgb(0x103529), ChipActiveBorder: rgb(0x103529), ChipActiveText: rgb(0xd9fdd3),

	Text: rgb(0xfafafa), TextSecondary: rgb(0xa2a2a2), Icon: rgb(0xa2a2a2), IconActive: rgb(0xfafafa),
	IconStrong: rgb(0xfafafa),
	Green:      rgb(0x21c063), OnGreen: rgb(0x0a0a0a),
	RailActive: rgb(0x333535), RailSeparator: rgb(0x2f3131),

	Doodle:   rgb(0x292c2b),
	BubbleIn: rgb(0x242626), BubbleOut: rgb(0x144d37),
	MetaIn: rgb(0xa7a8a8), MetaOut: rgb(0xa1b8af), TickRead: rgb(0x53bdeb),
	Link:    rgb(0x21c063),
	TextOut: rgb(0xfafafa), SecondaryOut: rgb(0xa1b8af),
	QuoteIn: rgb(0x1d1e1e), QuoteOut: rgb(0x103e2c),
	DateChip: rgb(0x1d1f1f), DateChipText: rgb(0xa5a5a5),
	UnreadBand: argb(0x000000, 0x4c),
	Encryption: rgb(0x1d1f1f), EncryptionText: rgb(0xffd279),
	Composer: rgb(0x242626), ComposerHint: rgb(0xa7a8a8),
	GroupAvatar: rgb(0x092642), GroupAvatarIcon: rgb(0x53a6fd),
	UserAvatar: rgb(0x242626), UserAvatarIcon: rgb(0xbdbdbd),
	AvatarBgs:     rgbs(0x36192a, 0x092c3d, 0x362c1f, 0x242447, 0x092d2f, 0x35221e, 0x092642, 0x35271e, 0x321622, 0x103529),
	GeneralAvatar: rgb(0xd1d8da),
	Banner:        rgb(0x2e2f2f), BannerText: rgb(0xd0d2d2), QRFg: rgb(0x122e31),
	Menu: rgb(0x242626), MenuHover: rgb(0x2e2f2f), Shadow: argb(0x000000, 0x60),
	CloseHover: rgb(0xc42b1c),
	Senders:    rgbs(0xff72a1, 0x53bdeb, 0xffd279, 0xa791ff, 0x42c7b8, 0xfc9775, 0x53a6fd, 0xdba685, 0xfb5061, 0x25d366),
	Members: rgbs(0x06cf9c, 0x53bdeb, 0xe26ab6, 0xa5b337, 0xfc9775, 0x53a6fd, 0xffb938, 0x25d366, 0xa791ff,
		0xfb5061, 0x7f66ff, 0x42c7b8, 0xff72a1, 0xd88deb, 0xfa6533, 0x02a698, 0xffd279),

	EmptyIcon: rgb(0x454545), RowHover: rgb(0x2d2e2e),
	Danger: rgb(0xfb5061), DangerSoft: rgb(0xfa99a4),
	RingViewed: rgb(0x757778), RingAudience: rgb(0xc15add), Verified: rgb(0x0085f4),
	AnnounceBg: rgb(0x342c21), AnnounceIcon: rgb(0xf6d78b),
	ChannelAvatar: rgb(0x32281e), ChannelAvatarIcon: rgb(0xd3a887),
	StatusBg: rgb(0x0f0f0f),

	Popup: rgb(0x161717), PopupBorder: rgb(0x2d2e2e), PopupHover: rgb(0x2d2e2e),
	PopupDivider: rgb(0x2d2e2e), PopupSub: rgb(0xa2a2a2),
	Picker: rgb(0x1d1f1f), PickerTab: rgb(0x242626), PickerTabBorder: rgb(0x343636),
	Viewer: rgb(0x171717), ViewerDivider: rgb(0x1f1f1f),
	ViewerThumb: rgb(0x242625), ViewerArrow: rgb(0x0e0e0e),
	Scrim: argb(0x000000, 0x93), Dialog: rgb(0x1d1f1f),
	Toast: rgb(0xeeeeee), ToastText: rgb(0x0a0a0a),
	BubbleLine: argb(0xffffff, 0x14), BubbleButton: rgb(0x53bdeb),
	CodeBg:      argb(0xffffff, 0x12),
	MentionPill: argb(0x21c063, 0x24),
	Selection:   rgb(0x1e3a9e),
}

var lightPalette = Palette{
	Frame: rgb(0xf7f5f3), FrameText: rgb(0x0a0a0a),
	PanelBorder: rgb(0xe3e0dc), Divider: rgb(0xe6e6e6),
	Panel: rgb(0xffffff), ChatBg: rgb(0xf5f1eb),
	Hover: rgb(0xf6f5f4), Selected: rgb(0xf6f5f4), Search: rgb(0xf6f5f4),
	Chip: rgb(0xffffff), ChipBorder: rgb(0xcccccc), ChipText: rgb(0x666666),
	ChipActive: rgb(0xd9fdd3), ChipActiveBorder: rgb(0xd9fdd3), ChipActiveText: rgb(0x15603e),

	Text: rgb(0x0a0a0a), TextSecondary: rgb(0x666666), Icon: rgb(0x666666), IconActive: rgb(0x0a0a0a),
	IconStrong: rgb(0x0a0a0a),
	Green:      rgb(0x1daa61), OnGreen: rgb(0xffffff),
	RailActive: rgb(0xe8e6e3), RailSeparator: rgb(0xe3e0dc),

	Doodle:   rgb(0xe8e1d8),
	BubbleIn: rgb(0xffffff), BubbleOut: rgb(0xd9fdd3),
	MetaIn: rgb(0x666666), MetaOut: rgb(0x576554), TickRead: rgb(0x007bfc),
	Link:    rgb(0x1b8755),
	TextOut: rgb(0x0a0a0a), SecondaryOut: rgb(0x576554),
	QuoteIn: rgb(0xf6f5f4), QuoteOut: rgb(0xd6f3cf),
	DateChip: rgb(0xfefefd), DateChipText: rgb(0x666666),
	UnreadBand: argb(0xffffff, 0x40),
	Encryption: rgb(0xfff0d4), EncryptionText: rgb(0x666055),
	Composer: rgb(0xffffff), ComposerHint: rgb(0x666666),
	GroupAvatar: rgb(0xd2e8fe), GroupAvatarIcon: rgb(0x0063cb),
	UserAvatar: rgb(0xeeeeee), UserAvatarIcon: rgb(0x757778),
	AvatarBgs:     rgbs(0xffdae7, 0xcaecfa, 0xfff0d4, 0xe8e0ff, 0xcbf2ee, 0xfee2d8, 0xd2e8fe, 0xf4ded1, 0xfbd8dc, 0xd9fdd3),
	GeneralAvatar: rgb(0xdfe5e7),
	Banner:        rgb(0xfff4c5), BannerText: rgb(0x54656f), QRFg: rgb(0x122e31),
	Menu: rgb(0xffffff), MenuHover: rgb(0xf5f6f6), Shadow: argb(0x0b141a, 0x30),
	CloseHover: rgb(0xc42b1c),
	Senders:    rgbs(0xd42a66, 0x027eb5, 0x9d6c2c, 0x5e47de, 0x028377, 0xc4532d, 0x0063cb, 0x855538, 0xb80531, 0x1b8755),
	Members: rgbs(0x06cf9c, 0x53bdeb, 0xe542a3, 0xa5b337, 0x007bfc, 0xc58730, 0x1daa61, 0x7f66ff, 0x5e47de,
		0x25d366, 0xc4532d, 0xff2e74, 0x7d9ef1, 0xfa6533, 0x02a698, 0xb4876e, 0xffb938),

	EmptyIcon: rgb(0xc4c9cc), RowHover: rgb(0xf6f5f4),
	Danger: rgb(0xea0038), DangerSoft: rgb(0xb80531),
	RingViewed: rgb(0xc2bdb8), RingAudience: rgb(0xc15add), Verified: rgb(0x0085f4),
	AnnounceBg: rgb(0xfdf1d8), AnnounceIcon: rgb(0xc58a13),
	ChannelAvatar: rgb(0xf6e7da), ChannelAvatarIcon: rgb(0xa86f45),
	StatusBg: rgb(0x0f0f0f),

	Popup: rgb(0xffffff), PopupBorder: rgb(0xe6e6e6), PopupHover: rgb(0xf9f8f8),
	PopupDivider: rgb(0xe6e6e6), PopupSub: rgb(0x666666),
	Picker: rgb(0xffffff), PickerTab: rgb(0xf0f2f5), PickerTabBorder: rgb(0xe3e0dc),
	Viewer: rgb(0xffffff), ViewerDivider: rgb(0xe9edef),
	ViewerThumb: rgb(0xf0f2f5), ViewerArrow: rgb(0xf0f2f5),
	Scrim: argb(0x000000, 0x93), Dialog: rgb(0xffffff),
	Toast: rgb(0x242626), ToastText: rgb(0xffffff),
	BubbleLine: argb(0x000000, 0x14), BubbleButton: rgb(0x027eb5),
	CodeBg:      argb(0x000000, 0x0f),
	MentionPill: argb(0x1daa61, 0x22),
	Selection:   rgb(0xb3d4fc),
}

// personTint is the index in Senders and AvatarBgs of someone, or of a group,
// from their ID: its user's last digit, like WhatsApp
// (useWAWebDefaultProfileColors in WhatsApp Web), so it's the same on every
// device. Only an ID without a digit there, which WhatsApp doesn't have (demo
// data), takes a hash, and no ID at all a hash of the name.
func personTint(id, name string) int {
	user := id
	if i := strings.IndexAny(user, ":@"); i >= 0 {
		user = user[:i]
	}
	if n := len(user); n > 0 && user[n-1] >= '0' && user[n-1] <= '9' {
		return int(user[n-1] - '0')
	}
	if id == "" {
		id = name
	}
	h := fnv.New32a()
	h.Write([]byte(id))
	return int(h.Sum32() % 10)
}

// Typeface prefers Segoe UI (what WhatsApp Desktop uses on Windows), color
// emoji from Noto, then common system UI fonts and the bundled Go fonts.
//
// MS Gothic covers Japanese and most Chinese text in a single file for
// every weight. Without it the fallback picks Yu Gothic, whose weights are
// separate files that each cost ~40 MB of heap once loaded (the whole
// glyph table is parsed), so a bold name plus a regular message would load
// two of them.
const typeface font.Typeface = "Segoe UI, " + emojiTypeface + ", Segoe UI Symbol, MS Gothic, Cambria Math, Helvetica Neue, Roboto, Noto Sans, sans-serif, Go"

// newShaper returns a shaper of the bundled fonts and the system's.
func newShaper() *text.Shaper { return text.NewShaper(text.WithCollection(bundledFonts())) }

func newTheme() *material.Theme {
	th := material.NewTheme()
	// System fonts are enabled by default and cover other scripts.
	th.Shaper = newShaper()
	th.Face = typeface
	th.TextSize = 14
	return th
}
