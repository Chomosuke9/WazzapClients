//go:build !windows

package filepick

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// open runs the desktop's file dialog tool: osascript on macOS, zenity or
// kdialog elsewhere.
func open(title string, multiple bool, filters []Filter) ([]string, error) {
	var cmd *exec.Cmd
	switch {
	case runtime.GOOS == "darwin":
		script := `set fs to choose file with prompt "` + strings.ReplaceAll(title, `"`, `'`) + `"`
		if multiple {
			script += ` with multiple selections allowed`
		}
		script += "\nset out to \"\"\nrepeat with f in (fs as list)\nset out to out & POSIX path of f & linefeed\nend repeat\nreturn out"
		cmd = exec.Command("osascript", "-e", script)
	case has("zenity"):
		args := []string{"--file-selection", "--title=" + title, "--separator=\n"}
		if multiple {
			args = append(args, "--multiple")
		}
		for _, f := range filters {
			pat := "*"
			if len(f.Exts) > 0 {
				pat = "*." + strings.Join(f.Exts, " *.")
			}
			args = append(args, "--file-filter="+f.Name+" | "+pat)
		}
		cmd = exec.Command("zenity", args...)
	case has("kdialog"):
		args := []string{"--title", title, "--getopenfilename", "."}
		if len(filters) > 0 && len(filters[0].Exts) > 0 {
			args = append(args, "*."+strings.Join(filters[0].Exts, " *.")+"|"+filters[0].Name)
		}
		if multiple {
			args = append(args, "--multiple", "--separate-output")
		}
		cmd = exec.Command("kdialog", args...)
	default:
		return nil, ErrUnsupported
	}
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return nil, nil // cancelled
	}
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			paths = append(paths, line)
		}
	}
	return paths, nil
}

func has(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
