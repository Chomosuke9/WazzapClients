package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// isTerminal reports whether f is a terminal, not a file, a pipe or
// /dev/null (a service's).
func isTerminal(f *os.File) bool {
	_, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS)
	return err == nil
}

func ansiTerminal(f *os.File) bool { return isTerminal(f) }

// headlessConsole: a terminal is where it was started from already.
func headlessConsole() {}
