//go:build !windows && !darwin

package desktop

import (
	"os/exec"
	"strings"
)

// systemDark asks GNOME's (and most other desktops') color-scheme setting,
// the one the XDG appearance portal reports: 'prefer-dark', 'prefer-light'
// or 'default' (no preference, which means light).
func systemDark() (dark, ok bool) {
	out, err := exec.Command("gsettings", "get", "org.gnome.desktop.interface", "color-scheme").Output()
	if err != nil {
		return false, false
	}
	return strings.Contains(string(out), "dark"), true
}
