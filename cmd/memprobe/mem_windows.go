package main

import (
	"syscall"
	"time"
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

// memory returns the process's private working set (what Task Manager
// shows), its working set and its private commit, in bytes.
func memory() (private, ws, commit uint64) {
	var c memCounters
	c.cb = uint32(unsafe.Sizeof(c))
	h, _ := syscall.GetCurrentProcess()
	getMemInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&c)), uintptr(c.cb))
	return c.privateWS, uint64(c.ws), uint64(c.private)
}

var getProcessTimes = syscall.NewLazyDLL("kernel32.dll").NewProc("GetProcessTimes")

// cpuTime returns the CPU time the process has used, user and kernel.
func cpuTime() time.Duration {
	var created, exited, kernel, user syscall.Filetime
	h, _ := syscall.GetCurrentProcess()
	getProcessTimes.Call(uintptr(h), uintptr(unsafe.Pointer(&created)), uintptr(unsafe.Pointer(&exited)),
		uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	ticks := func(f syscall.Filetime) int64 { return int64(f.HighDateTime)<<32 | int64(f.LowDateTime) }
	return time.Duration(ticks(kernel)+ticks(user)) * 100
}
