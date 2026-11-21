package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// isTerminal reports whether f is a console, not a file or a pipe.
func isTerminal(f *os.File) bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(f.Fd()), &mode) == nil
}

// ansiTerminal reports whether f is a console that takes escape codes,
// turning them on (Windows 10 and later).
func ansiTerminal(f *os.File) bool {
	h := windows.Handle(f.Fd())
	var mode uint32
	if windows.GetConsoleMode(h, &mode) != nil {
		return false
	}
	const vt = windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	return mode&vt != 0 || windows.SetConsoleMode(h, mode|vt) == nil
}

// headlessConsole gives the headless mode the console it was started
// from: a release is a GUI program (-H windowsgui), which gets none, so
// it would print nowhere. Output redirected to a file stays there. The
// console's input stays the shell's: link with -phone or /login.
func headlessConsole() {
	if h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); err == nil && h != 0 && h != windows.InvalidHandle {
		return
	}
	attach := windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")
	const parent = ^uintptr(0) // ATTACH_PARENT_PROCESS
	if r, _, _ := attach.Call(parent); r == 0 {
		return
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = f, f
	}
}
