//go:build windows && (amd64 || arm64)

package desktop

import (
	"syscall"
	"unsafe"
)

var procSetWindowDisplayAffinity = user32.NewProc("SetWindowDisplayAffinity")

const (
	wdaNone               = 0
	wdaMonitor            = 1    // captures show the window black
	wdaExcludeFromCapture = 0x11 // captures leave it out (Windows 10 2004 on)
)

var captureMsg uintptr // asks the window's thread to set its affinity

// BlockCapture keeps the window hwnd out of screenshots and screen
// recordings while on is set. The window's own thread does it, through
// the subclass EnableDrop installs, as it doesn't wait for the window.
func BlockCapture(hwnd uintptr, on bool) {
	dropMu.Lock()
	if captureMsg == 0 {
		name, _ := syscall.UTF16PtrFromString("WazzapClientsBlockCapture")
		captureMsg, _, _ = procRegisterWindowMsgDnD.Call(uintptr(unsafe.Pointer(name)))
	}
	_, sub := dropOld[hwnd]
	msg := captureMsg
	dropMu.Unlock()
	if hwnd == 0 {
		return
	}
	if !sub || msg == 0 {
		setAffinity(hwnd, on)
		return
	}
	procPostMessageW.Call(hwnd, msg, uintptr(boolInt(on)), 0)
}

// setAffinity runs on the window's thread. Systems before Windows 10 2004
// can't leave a window out, so captures show it black there.
func setAffinity(hwnd uintptr, on bool) {
	if !on {
		procSetWindowDisplayAffinity.Call(hwnd, wdaNone)
		return
	}
	if r, _, _ := procSetWindowDisplayAffinity.Call(hwnd, wdaExcludeFromCapture); r == 0 {
		procSetWindowDisplayAffinity.Call(hwnd, wdaMonitor)
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
