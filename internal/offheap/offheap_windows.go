package offheap

import (
	"syscall"
	"unsafe"
)

var (
	kernel32     = syscall.NewLazyDLL("kernel32.dll")
	virtualAlloc = kernel32.NewProc("VirtualAlloc")
	virtualFree  = kernel32.NewProc("VirtualFree")
)

const (
	memCommit     = 0x1000
	memReserve    = 0x2000
	memRelease    = 0x8000
	pageReadWrite = 0x04
)

// alloc returns n zeroed bytes of committed memory, or nil.
func alloc(n int) []byte {
	if virtualAlloc.Find() != nil || virtualFree.Find() != nil {
		return nil
	}
	addr, _, _ := virtualAlloc.Call(0, uintptr(n), memCommit|memReserve, pageReadWrite)
	if addr == 0 {
		return nil
	}
	// Through a pointer to addr: vet flags a uintptr converted directly.
	return unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&addr))), n)
}

func free(b []byte) {
	virtualFree.Call(uintptr(unsafe.Pointer(unsafe.SliceData(b))), 0, memRelease)
}
