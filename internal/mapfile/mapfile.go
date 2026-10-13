// Package mapfile maps a file into memory, copy-on-write: its pages stay
// in the file cache (shared and evictable) instead of the heap, and only a
// page written to becomes private memory.
package mapfile

import (
	"errors"
	"os"
)

// maxSize bounds what Map maps.
const maxSize = 1 << 30

var errSize = errors.New("mapfile: empty or too big")

// Map maps the file at path for good: the mapping is never released, so
// slices of it can be kept anywhere. Where mapping isn't supported, the
// file is read into the heap.
func Map(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() == 0 || st.Size() > maxSize {
		return nil, errSize
	}
	return mapFile(f, st.Size())
}
