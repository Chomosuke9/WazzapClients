package model

// A chat's theme can be set for everyone in it, and then each of their
// devices shows it (WhatsApp's "for everyone").

// SharedTheme is a look set for everyone in a chat. What it leaves out
// stays as each person has it.
type SharedTheme struct {
	// Reset drops the chat's look, the one a person chose included, for
	// the one their other chats have; the rest applies on top of that.
	Reset bool
	// Scheme is WhatsApp's color scheme ID ("Pink@Tonal"), SchemeDefault for
	// WhatsApp's own colors, or "" for the bubbles the chat has.
	Scheme string
	Wall   SharedWall
}

// SchemeDefault is the color scheme of a chat without a theme.
const SchemeDefault = "Default"

// WallKind is the kind of a SharedWall.
type WallKind int

const (
	WallKeep    WallKind = iota // the wallpaper the chat has
	WallDefault                 // the scheme's
	WallColor
	WallPhoto
)

// SharedWall is the wallpaper of a SharedTheme.
type SharedWall struct {
	Kind WallKind
	// Doodles says whether a WallDefault or WallColor has them.
	Doodles bool
	// Light and Dark are a WallColor's colors by mode, 0xRRGGBB, and
	// DoodleLight and DoodleDark its doodles', 0xAARRGGBB, or 0 to go with
	// the color.
	Light, Dark             uint32
	DoodleLight, DoodleDark uint32
	// Photo is a WallPhoto's file.
	Photo string
}

// ChatThemeEvent says a chat's shared theme changed.
type ChatThemeEvent struct{ ChatID string }

func (ChatThemeEvent) isEvent() {}
