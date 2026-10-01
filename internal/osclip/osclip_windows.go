//go:build windows

package osclip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32  = syscall.NewLazyDLL("user32.dll")
	kernel  = syscall.NewLazyDLL("kernel32.dll")
	shell32 = syscall.NewLazyDLL("shell32.dll")

	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procSetClipboardData           = user32.NewProc("SetClipboardData")
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	procRegisterClipboardFormatW   = user32.NewProc("RegisterClipboardFormatW")
	procGlobalLock                 = kernel.NewProc("GlobalLock")
	procGlobalUnlock               = kernel.NewProc("GlobalUnlock")
	procGlobalSize                 = kernel.NewProc("GlobalSize")
	procGlobalAlloc                = kernel.NewProc("GlobalAlloc")
	procGlobalFree                 = kernel.NewProc("GlobalFree")
	procDragQueryFileW             = shell32.NewProc("DragQueryFileW")
)

const (
	cfDIB         = 8
	cfUnicodeText = 13
	cfHDROP       = 15
	gmemMoveable  = 0x2
	biRGB         = 0
	biBitfields   = 3
)

// cfPNG is the "PNG" format browsers and Office put pictures in, with
// their transparency.
var cfPNG = func() uintptr {
	name, _ := syscall.UTF16PtrFromString("PNG")
	f, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(name)))
	return f
}()

// open opens the clipboard, waiting a little while another app has it.
func open(owner uintptr) bool {
	for range 20 {
		if r, _, _ := procOpenClipboard.Call(owner); r != 0 {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// pointer turns memory Windows handed out into a pointer, without go
// vet's complaint about converting a uintptr.
func pointer(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }

func closeClipboard() { procCloseClipboard.Call() }

func available(f uintptr) bool {
	r, _, _ := procIsClipboardFormatAvailable.Call(f)
	return r != 0
}

func hasText() bool { return available(cfUnicodeText) }

func files() []string {
	if !available(cfHDROP) || !open(0) {
		return nil
	}
	defer closeClipboard()
	h, _, _ := procGetClipboardData.Call(cfHDROP)
	if h == 0 {
		return nil
	}
	return DropFiles(h)
}

// DropFiles lists the paths in an HDROP, from the clipboard or a drop.
func DropFiles(h uintptr) []string {
	n, _, _ := procDragQueryFileW.Call(h, 0xFFFFFFFF, 0, 0)
	var out []string
	for i := range n {
		l, _, _ := procDragQueryFileW.Call(h, i, 0, 0)
		if l == 0 {
			continue
		}
		buf := make([]uint16, l+1)
		procDragQueryFileW.Call(h, i, uintptr(unsafe.Pointer(&buf[0])), l+1)
		out = append(out, syscall.UTF16ToString(buf))
	}
	return out
}

// globalBytes copies the contents of a global memory handle.
func globalBytes(h uintptr) []byte {
	size, _, _ := procGlobalSize.Call(h)
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 || size == 0 {
		return nil
	}
	defer procGlobalUnlock.Call(h)
	return bytes.Clone(unsafe.Slice((*byte)(pointer(p)), size))
}

func readImage() image.Image {
	pngOK := cfPNG != 0 && available(cfPNG)
	if !pngOK && !available(cfDIB) {
		return nil
	}
	if !open(0) {
		return nil
	}
	var data []byte
	isPNG := false
	if pngOK {
		if h, _, _ := procGetClipboardData.Call(cfPNG); h != 0 {
			data, isPNG = globalBytes(h), true
		}
	}
	if data == nil {
		if h, _, _ := procGetClipboardData.Call(cfDIB); h != 0 {
			data = globalBytes(h)
		}
	}
	closeClipboard()
	if isPNG {
		if img, err := png.Decode(bytes.NewReader(data)); err == nil {
			return img
		}
		return nil
	}
	return parseDIB(data)
}

// parseDIB reads a packed DIB (BITMAPINFOHEADER or a later version, then
// the pixels) of 24 or 32 bits a pixel.
func parseDIB(b []byte) image.Image {
	le32 := binary.LittleEndian.Uint32
	if len(b) < 40 {
		return nil
	}
	hdr := int(le32(b))
	w, h := int(int32(le32(b[4:]))), int(int32(le32(b[8:])))
	bpp := int(binary.LittleEndian.Uint16(b[14:]))
	comp := le32(b[16:])
	if w <= 0 || h == 0 || hdr < 40 || hdr > len(b) || (bpp != 24 && bpp != 32) {
		return nil
	}
	topDown := h < 0
	h = max(h, -h)
	rm, gm, bm, am := uint32(0xff0000), uint32(0xff00), uint32(0xff), uint32(0)
	off := hdr
	if comp == biBitfields {
		if len(b) < 52 {
			return nil
		}
		rm, gm, bm = le32(b[40:]), le32(b[44:]), le32(b[48:])
		if hdr == 40 {
			off += 12 // the masks follow the header
		} else if len(b) >= 56 {
			am = le32(b[52:])
		}
	} else if comp != biRGB {
		return nil
	}
	stride := (w*bpp + 31) / 32 * 4
	if off+stride*h > len(b) || w*h > 1<<28 {
		return nil
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	type field struct{ shift, mask uint32 }
	fieldOf := func(mask uint32) field {
		f := field{}
		for mask != 0 && mask&1 == 0 {
			mask >>= 1
			f.shift++
		}
		f.mask = mask
		return f
	}
	ch := func(px uint32, f field) uint8 {
		if f.mask == 0 {
			return 0
		}
		return uint8((px >> f.shift) & f.mask * 255 / f.mask)
	}
	rf, gf, bf, af := fieldOf(rm), fieldOf(gm), fieldOf(bm), fieldOf(am)
	anyAlpha := false
	for y := range h {
		row := b[off+y*stride:]
		dy := h - 1 - y
		if topDown {
			dy = y
		}
		dst := img.Pix[dy*img.Stride:]
		for x := range w {
			var px uint32
			if bpp == 32 {
				px = le32(row[4*x:])
			} else {
				px = uint32(row[3*x]) | uint32(row[3*x+1])<<8 | uint32(row[3*x+2])<<16
			}
			d := dst[4*x : 4*x+4 : 4*x+4]
			d[0], d[1], d[2] = ch(px, rf), ch(px, gf), ch(px, bf)
			d[3] = 0xff
			if bpp == 32 {
				a := uint8(px >> 24)
				if am != 0 {
					a = ch(px, af)
				}
				d[3] = a
				anyAlpha = anyAlpha || a != 0
			}
		}
	}
	// Most apps leave the fourth byte of a plain DIB at zero.
	if bpp == 32 && !anyAlpha {
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 0xff
		}
	}
	return img
}

func writeImage(owner uintptr, img image.Image) error {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return errors.New("osclip: empty image")
	}
	// A plain 32-bit DIB on white, bottom-up, which every app reads.
	dib := make([]byte, 40+w*h*4)
	le := binary.LittleEndian
	le.PutUint32(dib, 40)
	le.PutUint32(dib[4:], uint32(w))
	le.PutUint32(dib[8:], uint32(h))
	le.PutUint16(dib[12:], 1)
	le.PutUint16(dib[14:], 32)
	le.PutUint32(dib[20:], uint32(w*h*4))
	flat := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(flat, flat.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), img, b.Min, draw.Over)
	for y := range h {
		src := flat.Pix[y*flat.Stride:]
		dst := dib[40+(h-1-y)*w*4:]
		for x := range w {
			s := src[4*x:]
			dst[4*x], dst[4*x+1], dst[4*x+2], dst[4*x+3] = s[2], s[1], s[0], 0xff
		}
	}
	var pngData bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&pngData, img); err != nil {
		pngData.Reset()
	}
	if !open(owner) {
		return errors.New("osclip: the clipboard is busy")
	}
	defer closeClipboard()
	if r, _, err := procEmptyClipboard.Call(); r == 0 {
		return err
	}
	if err := setData(cfDIB, dib); err != nil {
		return err
	}
	if cfPNG != 0 && pngData.Len() > 0 {
		_ = setData(cfPNG, pngData.Bytes())
	}
	return nil
}

func setData(format uintptr, data []byte) error {
	h, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(len(data)))
	if h == 0 {
		return err
	}
	p, _, err := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return err
	}
	copy(unsafe.Slice((*byte)(pointer(p)), len(data)), data)
	procGlobalUnlock.Call(h)
	if r, _, err := procSetClipboardData.Call(format, h); r == 0 {
		procGlobalFree.Call(h) // still ours when SetClipboardData fails
		return err
	}
	return nil
}
