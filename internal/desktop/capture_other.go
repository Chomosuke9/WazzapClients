//go:build !windows || !(amd64 || arm64)

package desktop

// BlockCapture does nothing here: this system has no way to keep a window
// out of screenshots.
func BlockCapture(hwnd uintptr, on bool) {}
