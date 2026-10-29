//go:build windows && !386

package video

import (
	"math"
	"syscall"
)

// withDouble appends f to args as the C calling convention passes it: in
// a register of its own.
func withDouble(args []uintptr, f float64) []uintptr {
	return append(args, uintptr(math.Float64bits(f)))
}

// callDouble calls the COM method fn of this, which returns a double. It
// comes back in XMM0, which syscall hands back as r2.
func callDouble(fn, this uintptr) float64 {
	_, r2, _ := syscall.SyscallN(fn, this)
	return math.Float64frombits(uint64(r2))
}
