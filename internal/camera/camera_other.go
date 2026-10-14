//go:build !windows

package camera

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// apps are the camera apps tried in turn, with where each saves (under
// the home folder).
var apps = []struct {
	cmd  []string
	dirs []string
}{
	{[]string{"snapshot"}, []string{"Pictures/Camera", "Videos/Camera"}}, // GNOME Camera
	{[]string{"cheese"}, []string{"Pictures/Webcam", "Videos/Webcam"}},
	{[]string{"kamoso"}, []string{"Pictures", "Videos"}},
}

// launch starts Photo Booth on macOS, or the first camera app found
// elsewhere.
func launch() ([]string, error) {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		if err := start("open", "-a", "Photo Booth"); err != nil {
			return nil, ErrUnsupported
		}
		return []string{filepath.Join(home, "Pictures", "Photo Booth Library", "Pictures")}, nil
	}
	for _, a := range apps {
		if _, err := exec.LookPath(a.cmd[0]); err != nil {
			continue
		}
		if err := start(a.cmd[0], a.cmd[1:]...); err != nil {
			continue
		}
		var dirs []string
		for _, d := range a.dirs {
			dirs = append(dirs, filepath.Join(home, d))
		}
		return dirs, nil
	}
	return nil, ErrUnsupported
}

func start(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
