//go:build linux

package ui

import (
	"gioui.org/app"
	"gioui.org/io/event"
)

// windowHandle returns what tells a view event's window from another's,
// its Wayland surface or X11 window, or 0: the host turns on what a new
// window needs (dropped files, see desktop.EnableDrop) when it changes.
func windowHandle(e event.Event) uintptr {
	switch e := e.(type) {
	case app.WaylandViewEvent:
		return uintptr(e.Surface)
	case app.X11ViewEvent:
		return e.Window
	}
	return 0
}
