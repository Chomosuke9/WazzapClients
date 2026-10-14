//go:build !windows && !linux && !freebsd && !openbsd && !netbsd

package record

import "os/exec"

const supported = false

func openMic(rate int, sink func([]byte), fail func(error)) (source, error) {
	return nil, ErrUnsupported
}

func hideWindow(*exec.Cmd) {}
