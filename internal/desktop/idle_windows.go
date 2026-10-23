//go:build windows

package desktop

import (
	"time"
	"unsafe"
)

var (
	procGetLastInputInfo = user32.NewProc("GetLastInputInfo")
	procGetTickCount     = kernel32.NewProc("GetTickCount")
)

// IdleTime is how long the user hasn't touched the keyboard or mouse,
// in any app. It keeps growing while the screen is locked.
func IdleTime() time.Duration {
	info := struct{ size, time uint32 }{size: 8}
	if r, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		return 0
	}
	now, _, _ := procGetTickCount.Call()
	return time.Duration(uint32(now)-info.time) * time.Millisecond // wraps every 49.7 days
}
