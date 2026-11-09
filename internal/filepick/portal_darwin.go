package filepick

import "errors"

var errNoPortal = errors.New("filepick: no file chooser portal")

// portalOpen: macOS has no XDG portal; open uses osascript.
func portalOpen(string, bool, []Filter) ([]string, error) { return nil, errNoPortal }
