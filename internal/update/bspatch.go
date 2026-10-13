package update

import (
	"bytes"
	"compress/bzip2"
	"errors"
	"io"
)

// errPatch means a patch doesn't follow the bsdiff format.
var errPatch = errors.New("the update patch is damaged")

// bspatch writes to w the file that patch (bsdiff's BSDIFF40 format, made
// by cmd/mkpatch) turns old into. It reads old where the patch says and
// writes the new file as it goes, so neither file is held in memory.
//
// The format: "BSDIFF40", then the lengths of the bzip2'd control and
// diff blocks and the new file's size, then the three bzip2 blocks. The
// control block is triples (x, y, z): add x bytes of the diff block to x
// bytes of old, copy y bytes of the extra block, then move z bytes in old.
func bspatch(w io.Writer, old io.ReaderAt, oldSize int64, patch []byte) error {
	if len(patch) < 32 || string(patch[:8]) != "BSDIFF40" {
		return errPatch
	}
	ctrlLen, diffLen, newSize := offtin(patch[8:]), offtin(patch[16:]), offtin(patch[24:])
	if ctrlLen < 0 || diffLen < 0 || newSize < 0 || newSize > maxExe ||
		ctrlLen > int64(len(patch))-32 || diffLen > int64(len(patch))-32-ctrlLen {
		return errPatch
	}
	ctrl := bzip2.NewReader(bytes.NewReader(patch[32 : 32+ctrlLen]))
	diff := bzip2.NewReader(bytes.NewReader(patch[32+ctrlLen : 32+ctrlLen+diffLen]))
	extra := bzip2.NewReader(bytes.NewReader(patch[32+ctrlLen+diffLen:]))

	var head [24]byte
	buf := make([]byte, 64<<10)
	oldBuf := make([]byte, len(buf))
	var newPos, oldPos int64
	for newPos < newSize {
		if _, err := io.ReadFull(ctrl, head[:]); err != nil {
			return errPatch
		}
		add, copyLen, seek := offtin(head[0:]), offtin(head[8:]), offtin(head[16:])
		if add < 0 || copyLen < 0 || add > newSize-newPos || copyLen > newSize-newPos-add {
			return errPatch
		}
		for add > 0 {
			n := min(add, int64(len(buf)))
			if _, err := io.ReadFull(diff, buf[:n]); err != nil {
				return errPatch
			}
			// Bytes of old before its start or past its end add nothing.
			lo, hi := max(oldPos, 0), min(oldPos+n, oldSize)
			if lo < hi {
				o := oldBuf[:hi-lo]
				if _, err := old.ReadAt(o, lo); err != nil {
					return err
				}
				d := buf[lo-oldPos:]
				for i, b := range o {
					d[i] += b
				}
			}
			if _, err := w.Write(buf[:n]); err != nil {
				return err
			}
			add -= n
			newPos += n
			oldPos += n
		}
		if _, err := io.CopyN(w, extra, copyLen); err != nil {
			if errors.Is(err, io.EOF) {
				return errPatch
			}
			return err
		}
		newPos += copyLen
		oldPos += seek
	}
	return nil
}

// offtin reads one of bsdiff's numbers: 8 bytes, little endian, with the
// sign in the top bit (not two's complement).
func offtin(b []byte) int64 {
	var y int64
	for i := 6; i >= 0; i-- {
		y = y<<8 | int64(b[i])
	}
	y |= int64(b[7]&0x7f) << 56
	if b[7]&0x80 != 0 {
		y = -y
	}
	return y
}
