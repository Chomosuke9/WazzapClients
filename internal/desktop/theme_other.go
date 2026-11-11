//go:build !windows && !darwin

package desktop

import (
	"os/exec"
	"strings"

	"github.com/godbus/dbus/v5"
)

// systemDark asks the XDG desktop portal for the color scheme every
// desktop reports through it (GNOME, KDE, and wlroots compositors such as
// Hyprland and Sway): 1 is dark, 2 light and 0 no preference, which means
// light. Without a portal it asks GNOME's setting of the same name.
func systemDark() (dark, ok bool) {
	if conn, err := dbus.ConnectSessionBus(); err == nil {
		defer conn.Close()
		var v dbus.Variant
		err := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop").
			Call("org.freedesktop.portal.Settings.Read", 0, "org.freedesktop.appearance", "color-scheme").Store(&v)
		if err == nil {
			// Read wraps the value in a variant of its own.
			val := v.Value()
			if inner, ok := val.(dbus.Variant); ok {
				val = inner.Value()
			}
			if scheme, ok := val.(uint32); ok {
				return scheme == 1, true
			}
		}
	}
	out, err := exec.Command("gsettings", "get", "org.gnome.desktop.interface", "color-scheme").Output()
	if err != nil {
		return false, false
	}
	return strings.Contains(string(out), "dark"), true
}
