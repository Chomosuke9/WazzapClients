package sticker

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps ffmpeg, a console program, from flashing a console
// window: the app has none to share.
func hideWindow(cmd *exec.Cmd) {
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
