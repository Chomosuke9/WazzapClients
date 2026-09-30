// Package memtrim gives memory back to the operating system while the app
// is idle.
package memtrim

import "runtime/debug"

// Trim returns free heap memory to the OS and, where the OS supports it,
// trims the process working set.
//
// Much of a GUI process's resident memory is memory it no longer touches:
// heap spans freed since the last busy moment, and graphics driver pools
// left over from textures and buffers that were released. Trimming moves
// those pages out; pages still in use come back on the next frame, which
// costs a few milliseconds of soft page faults.
func Trim() {
	debug.FreeOSMemory()
	trimWorkingSet()
}
