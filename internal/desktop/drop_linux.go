//go:build linux

package desktop

import "gioui.org/app"

// DropHandler hears about files dragged onto the window.
type DropHandler struct {
	Over func(on bool)
	Drop func(paths []string)
}

// EnableDrop lets files be dropped on the window: Gio, patched
// (patches/gio.patch), takes them from Wayland's data device or X11's
// XDND and calls h on the window's thread. hwnd only says there is a
// window; the handler serves whichever one the app has.
func EnableDrop(hwnd uintptr, h DropHandler) {
	app.SetDropHandler(&app.DropHandler{Over: h.Over, Drop: h.Drop})
}
