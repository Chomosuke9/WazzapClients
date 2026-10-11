//go:build unix

package mapfile

import (
	"os"
	"syscall"
)

func mapFile(f *os.File, size int64) ([]byte, error) {
	return syscall.Mmap(int(f.Fd()), 0, int(size), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_PRIVATE)
}
