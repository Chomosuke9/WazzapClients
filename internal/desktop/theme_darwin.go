package desktop

import (
	"os/exec"
	"strings"
)

// systemDark reads the appearance from the global defaults: AppleInterfaceStyle
// is "Dark" in dark mode and missing (an error) in light mode.
func systemDark() (dark, ok bool) {
	out, err := exec.Command("defaults", "read", "-g", "AppleInterfaceStyle").Output()
	if err != nil {
		if _, exited := err.(*exec.ExitError); exited {
			return false, true
		}
		return false, false
	}
	return strings.TrimSpace(string(out)) == "Dark", true
}
