//go:build windows

package video

import (
	"math"
	"syscall"
	"unsafe"
)

// 32-bit x86 passes a double as two words on the stack and returns one on
// the x87 stack (ST0), which syscall.SyscallN can't read. doubleThunk, in
// assembly, reads it.

// withDouble appends f to args as the C calling convention passes it.
func withDouble(args []uintptr, f float64) []uintptr {
	b := math.Float64bits(f)
	return append(args, uintptr(uint32(b)), uintptr(b>>32))
}

// callDouble calls the COM method fn of this, which returns a double.
func callDouble(fn, this uintptr) float64 {
	var f float64
	syscall.SyscallN(doubleThunkAddr(), fn, this, uintptr(unsafe.Pointer(&f)))
	return f
}

// doubleThunk(fn, this, out) is only called through syscall.SyscallN, so
// it runs on the system stack the way Windows code expects. It calls fn
// with this and stores the double fn returns at out.
func doubleThunk(fn, this, out uintptr)

// doubleThunkAddr returns doubleThunk's address.
func doubleThunkAddr() uintptr
