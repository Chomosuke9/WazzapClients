// Package osclip reads files and pictures from the system clipboard, and
// puts pictures on it. Gio's own clipboard carries only text.
package osclip

import (
	"errors"
	"image"
)

// ErrUnsupported means this system's clipboard can't be read or written
// for files and pictures.
var ErrUnsupported = errors.New("osclip: not supported on this system")

// Files returns the files copied to the clipboard (in Explorer, say), or
// nil.
func Files() []string { return files() }

// HasText reports whether the clipboard holds text.
func HasText() bool { return hasText() }

// Image returns the picture on the clipboard (a screenshot, an image
// copied in a browser), or nil.
func Image() image.Image { return readImage() }

// SetImage puts img on the clipboard. owner is the window that owns the
// clipboard (an HWND on Windows); 0 is fine elsewhere.
func SetImage(owner uintptr, img image.Image) error { return writeImage(owner, img) }
