//go:build windows

package filepick

import (
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

var (
	comdlg32                 = syscall.NewLazyDLL("comdlg32.dll")
	ole32                    = syscall.NewLazyDLL("ole32.dll")
	procGetOpenFileNameW     = comdlg32.NewProc("GetOpenFileNameW")
	procCommDlgExtendedError = comdlg32.NewProc("CommDlgExtendedError")
	procCoInitializeEx       = ole32.NewProc("CoInitializeEx")
	procCoUninitialize       = ole32.NewProc("CoUninitialize")
)

// openFileName is OPENFILENAMEW (commdlg.h).
type openFileName struct {
	structSize    uint32
	owner         uintptr
	instance      uintptr
	filter        *uint16
	customFilter  *uint16
	maxCustFilter uint32
	filterIndex   uint32
	file          *uint16
	maxFile       uint32
	fileTitle     *uint16
	maxFileTitle  uint32
	initialDir    *uint16
	title         *uint16
	flags         uint32
	fileOffset    uint16
	fileExtension uint16
	defExt        *uint16
	custData      uintptr
	hook          uintptr
	templateName  *uint16
	reserved      uintptr
	reserved2     uint32
	flagsEx       uint32
}

const (
	ofnHideReadOnly     = 0x4
	ofnNoChangeDir      = 0x8
	ofnAllowMultiSelect = 0x200
	ofnPathMustExist    = 0x800
	ofnFileMustExist    = 0x1000
	ofnExplorer         = 0x80000

	coinitApartmentThreaded = 0x2
	coinitDisableOLE1DDE    = 0x4
)

func open(title string, multiple bool, filters []Filter) ([]string, error) {
	if comdlg32.Load() != nil {
		return nil, ErrUnsupported
	}
	// The dialog needs a single-threaded COM apartment on its thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if ole32.Load() == nil {
		procCoInitializeEx.Call(0, coinitApartmentThreaded|coinitDisableOLE1DDE)
		defer procCoUninitialize.Call()
	}

	// "Name\0*.a;*.b\0" pairs, ending with an extra \0.
	var f strings.Builder
	for _, flt := range filters {
		pat := "*.*"
		if len(flt.Exts) > 0 {
			pat = "*." + strings.Join(flt.Exts, ";*.")
		}
		f.WriteString(flt.Name + "\x00" + pat + "\x00")
	}
	f.WriteString("\x00")
	filter := syscall.StringToUTF16(f.String())
	t := syscall.StringToUTF16(title)
	buf := make([]uint16, 1<<16)
	ofn := openFileName{
		filter:      &filter[0],
		filterIndex: 1,
		file:        &buf[0],
		maxFile:     uint32(len(buf)),
		title:       &t[0],
		flags:       ofnExplorer | ofnFileMustExist | ofnPathMustExist | ofnNoChangeDir | ofnHideReadOnly,
	}
	ofn.structSize = uint32(unsafe.Sizeof(ofn))
	if multiple {
		ofn.flags |= ofnAllowMultiSelect
	}
	ok, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	runtime.KeepAlive(filter)
	runtime.KeepAlive(t)
	if ok == 0 {
		if code, _, _ := procCommDlgExtendedError.Call(); code != 0 {
			return nil, syscall.Errno(code)
		}
		return nil, nil // cancelled
	}
	// One path, or the folder followed by the file names, each ending
	// with \0 and the list with another \0.
	var parts []string
	for i := 0; i < len(buf) && buf[i] != 0; {
		j := i
		for j < len(buf) && buf[j] != 0 {
			j++
		}
		parts = append(parts, syscall.UTF16ToString(buf[i:j]))
		i = j + 1
	}
	if len(parts) <= 1 {
		return parts, nil
	}
	dir := strings.TrimRight(parts[0], `\`)
	out := make([]string, 0, len(parts)-1)
	for _, name := range parts[1:] {
		out = append(out, dir+`\`+name)
	}
	return out, nil
}
