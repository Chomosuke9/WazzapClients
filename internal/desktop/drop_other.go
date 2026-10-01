//go:build !windows || !(amd64 || arm64)

package desktop

// DropHandler hears about files dragged onto the window.
type DropHandler struct {
	Over func(on bool)
	Drop func(paths []string)
}

// EnableDrop does nothing here: dropping files on the window isn't
// supported on this system.
func EnableDrop(hwnd uintptr, h DropHandler) {}
