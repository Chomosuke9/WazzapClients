package camera

import (
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// launch starts Windows' Camera app, which saves to the Camera Roll.
func launch() ([]string, error) {
	dir, err := windows.KnownFolderPath(windows.FOLDERID_CameraRoll, windows.KF_FLAG_DEFAULT)
	if err != nil {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "Pictures", "Camera Roll")
	}
	cmd := exec.Command("explorer.exe", "microsoft.windows.camera:")
	if err := cmd.Start(); err != nil {
		return nil, ErrUnsupported
	}
	go cmd.Wait() // reap it; explorer hands the URI over and exits
	return []string{dir}, nil
}
