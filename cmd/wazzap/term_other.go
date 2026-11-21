//go:build !linux && !windows

package main

import "os"

// isTerminal reports whether f is a character device, as terminals are.
func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func ansiTerminal(f *os.File) bool { return isTerminal(f) }

func headlessConsole() {}
