//go:build !unix && !windows

package mapfile

import (
	"io"
	"os"
)

func mapFile(f *os.File, size int64) ([]byte, error) {
	b := make([]byte, size)
	_, err := io.ReadFull(f, b)
	return b, err
}
