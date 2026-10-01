package ui

import (
	"gioui.org/app"
	"gioui.org/io/event"
)

// windowHandle returns the HWND a view event brings, or 0.
func windowHandle(e event.Event) uintptr {
	if v, ok := e.(app.Win32ViewEvent); ok && v.Valid() {
		return v.HWND
	}
	return 0
}
