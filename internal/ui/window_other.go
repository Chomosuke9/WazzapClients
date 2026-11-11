//go:build !windows && !linux

package ui

import "gioui.org/io/event"

// windowHandle returns 0: this system's windows get no icon or dropped files (see desktop).
func windowHandle(e event.Event) uintptr { return 0 }
