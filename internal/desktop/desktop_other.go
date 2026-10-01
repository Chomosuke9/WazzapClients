//go:build !windows

package desktop

import "image"

// Lock always succeeds: other systems don't stop a second instance yet.
func Lock(dir string) bool { return true }

// TrayStart fails: there is no tray icon on other systems yet.
func TrayStart(t Tray) error { return ErrUnsupported }

// TrayStop does nothing.
func TrayStop() {}

// SetTooltip does nothing.
func SetTooltip(s string) {}

// SetWindowIcon does nothing.
func SetWindowIcon(hwnd uintptr, draw func(px int) *image.RGBA) {}

// StartAtLogin reports false.
func StartAtLogin() bool { return false }

// SetStartAtLogin fails.
func SetStartAtLogin(on bool, args []string) error { return ErrUnsupported }
