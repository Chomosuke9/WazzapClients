package memtrim

import "syscall"

var setWorkingSetSize = syscall.NewLazyDLL("kernel32.dll").NewProc("SetProcessWorkingSetSize")

func trimWorkingSet() {
	h, err := syscall.GetCurrentProcess()
	if err != nil || setWorkingSetSize.Find() != nil {
		return
	}
	// (SIZE_T)-1 for both sizes removes as many pages as possible.
	setWorkingSetSize.Call(uintptr(h), ^uintptr(0), ^uintptr(0))
}
