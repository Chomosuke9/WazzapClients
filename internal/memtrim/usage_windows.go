package memtrim

import (
	"syscall"
	"unsafe"
)

// PROCESS_MEMORY_COUNTERS_EX2
type memCounters struct {
	cb, pageFaults                  uint32
	peakWS, ws                      uintptr
	_, _, _, _                      uintptr
	pagefile, peakPagefile, private uintptr
	privateWS, sharedCommit         uint64
}

var getMemInfo = syscall.NewLazyDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")

// PrivateWorkingSet returns the process's private working set in bytes:
// the memory Task Manager shows. It is 0 where it can't be read.
func PrivateWorkingSet() uint64 {
	if getMemInfo.Find() != nil {
		return 0
	}
	var c memCounters
	c.cb = uint32(unsafe.Sizeof(c))
	h, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0
	}
	if ok, _, _ := getMemInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&c)), uintptr(c.cb)); ok == 0 {
		return 0
	}
	return c.privateWS
}
