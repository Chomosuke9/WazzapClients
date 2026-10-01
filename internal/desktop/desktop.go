// Package desktop ties the app into the desktop: a notification area
// (tray) icon that keeps it reachable while its window is closed, one
// instance per data directory, starting at login, and the window's icon.
//
// Only Windows has all of it. Elsewhere TrayStart fails, Lock always
// succeeds, and closing the window quits as before.
package desktop

import (
	"errors"
	"image"
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
}
