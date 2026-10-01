//go:build !windows

package ui

import "gioui.org/io/event"

// windowHandle returns 0: only Windows windows get an icon (see desktop).
func windowHandle(e event.Event) uintptr { return 0 }
