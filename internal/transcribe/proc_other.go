//go:build !windows

package transcribe

import "os/exec"

func background(*exec.Cmd) {}
