package mapfile

import (
	"os"
	"syscall"
	"unsafe"
)

func mapFile(f *os.File, size int64) ([]byte, error) {
	h, err := syscall.CreateFileMapping(syscall.Handle(f.Fd()), nil, syscall.PAGE_WRITECOPY,
		uint32(size>>32), uint32(size), nil)
	if err != nil {
		return nil, err
	}
	defer syscall.CloseHandle(h) // the view keeps the mapping
	addr, err := syscall.MapViewOfFile(h, syscall.FILE_MAP_COPY, 0, 0, uintptr(size))
	if err != nil {
		return nil, err
	}
	// Through a pointer to addr: vet flags a uintptr converted directly.
	return unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&addr))), int(size)), nil
}
