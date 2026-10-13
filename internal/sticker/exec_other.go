//go:build !windows

package sticker

import "os/exec"

func hideWindow(*exec.Cmd) {}
