package transcribe

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// background runs cmd without a console window, after everything else.
func background(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true,
		CreationFlags: windows.CREATE_NO_WINDOW | windows.BELOW_NORMAL_PRIORITY_CLASS}
}
