// Package desktop ties the app into the desktop: a notification area
// (tray) icon that keeps it reachable while its window is closed, one
// instance per data directory, starting at login, the window's icon, and
// opening WhatsApp's links (whatsapp://…) in the app.
//
// Windows and Linux have it. On Linux the tray icon is a StatusNotifierItem,
// which takes a panel that shows them (KDE's, waybar, GNOME with the
// AppIndicator extension): without one TrayStart fails and closing the
// window quits. Elsewhere TrayStart fails and Lock always succeeds.
package desktop

import (
	"errors"
	"image"
	"time"
)

// ErrUnsupported reports a system without a tray icon.
var ErrUnsupported = errors.New("desktop: not supported on this system")

// Tray describes the tray icon.
type Tray struct {
	// Name labels the icon's menu ("Open Name", "Quit Name") and is the
	// tooltip until SetTooltip changes it.
	Name string
	// Icon draws the icon px pixels square.
	Icon func(px int) *image.RGBA
	// Open is called when the icon is clicked, or Open is chosen from its
	// menu, and when another instance starts (see Lock). Quit is called
	// when Quit is chosen. Both run on the tray's own goroutine.
	Open, Quit func()
	// Link is called, on the tray's goroutine, with the link another
	// instance was started to open (see Lock).
	Link func(link string)
	// Activation is called, before Open or Link, with the token that
	// lets the window come to the front, when the click or the other
	// instance's launch brought one (Wayland's xdg-activation).
	Activation func(token string)
	// Wait is how long TrayStart waits for the system's tray to come up
	// before it gives up (Linux, where an app started at login can be
	// there before the panel).
	Wait time.Duration
}
