//go:build unix

package offheap

import "syscall"

// alloc returns n zeroed bytes of anonymous memory, or nil.
func alloc(n int) []byte {
	b, err := syscall.Mmap(-1, 0, n, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return nil
	}
	return b
}

func free(b []byte) { syscall.Munmap(b) }
