package ui

import "syscall"

// Gio makes only its window thread per-monitor DPI aware. The rest of the
// process stays DPI unaware, and while a drag holds the mouse capture,
// Windows reports the pointer in the unaware, scaled-down coordinates: at
// 150% scaling, a drag moved things two thirds as far as the pointer.
// Making the whole process aware, before any window exists, fixes that.
func init() {
	p := syscall.NewLazyDLL("user32.dll").NewProc("SetProcessDpiAwarenessContext")
	if p.Find() == nil {
		const perMonitorAwareV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
		p.Call(perMonitorAwareV2)
	}
}
