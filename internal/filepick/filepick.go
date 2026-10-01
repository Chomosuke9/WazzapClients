// Package filepick asks the user to pick files with the system's own
// "Open" dialog.
package filepick

import "errors"

// ErrUnsupported means there is no file dialog on this system.
var ErrUnsupported = errors.New("filepick: no file dialog on this system")

// Filter limits the dialog to files with the given extensions (without
// the dot). A filter without extensions matches every file.
type Filter struct {
	Name string
	Exts []string
}

// Open shows the dialog and blocks until it closes. It returns no paths
// when the user cancels. Call it from its own goroutine.
func Open(title string, multiple bool, filters ...Filter) ([]string, error) {
	return open(title, multiple, filters)
}
