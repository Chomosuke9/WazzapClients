//go:build windows && (amd64 || arm64)

package desktop

import (
	"sync"
	"syscall"
	"unsafe"
)

// Dropping files on the window. The window gets an OLE drop target, so
// the app hears about a drag as it enters (to show where to drop), not
// only once it's dropped as with WM_DROPFILES. OLE wants the target
// registered on the thread that owns the window, Gio's, so the window is
// subclassed and the registration posted to it as a message.
//
// The callbacks take POINTL by value, which is one register on 64-bit
// Windows; hence the build constraint.

var (
	ole32 = syscall.NewLazyDLL("ole32.dll")

	procOleInitialize        = ole32.NewProc("OleInitialize")
	procRegisterDragDrop     = ole32.NewProc("RegisterDragDrop")
	procRevokeDragDrop       = ole32.NewProc("RevokeDragDrop")
	procReleaseStgMedium     = ole32.NewProc("ReleaseStgMedium")
	procSetWindowLongPtrW    = user32.NewProc("SetWindowLongPtrW")
	procGetWindowLongPtrW    = user32.NewProc("GetWindowLongPtrW")
	procCallWindowProcW      = user32.NewProc("CallWindowProcW")
	procDragQueryFileW       = shell32.NewProc("DragQueryFileW")
	procRegisterWindowMsgDnD = user32.NewProc("RegisterWindowMessageW")
)

const (
	gwlpWndProc  = ^uintptr(3) // -4
	wmNCDestroy  = 0x0082
	cfHDROP      = 15
	dvaspContent = 1
	tymedHGlobal = 1
	sOK          = 0
	eNoInterface = 0x80004002

	dropEffectNone = 0
	dropEffectCopy = 1
	dropEffectLink = 4
)

type guid struct {
	d1     uint32
	d2, d3 uint16
	d4     [8]byte
}

var (
	iidUnknown    = guid{0, 0, 0, [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidDropTarget = guid{0x122, 0, 0, [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
)

type formatEtc struct {
	format uint16
	ptd    uintptr
	aspect uint32
	index  int32
	tymed  uint32
}

type stgMedium struct {
	tymed   uint32
	handle  uintptr
	release uintptr
}

// DropHandler hears about files dragged onto the window. Its functions
// run on the window's thread: they must return quickly and never wait
// for the UI goroutine.
type DropHandler struct {
	// Over is called with true when a drag carrying files enters the
	// window, and false when it leaves or drops.
	Over func(on bool)
	// Drop gets the dropped files' paths.
	Drop func(paths []string)
}

var (
	dropMu      sync.Mutex
	dropHandler DropHandler
	dropOld     = map[uintptr]uintptr{} // subclassed window -> its own procedure
	dropFiles   bool                    // the drag in progress carries files
	dropMsg     uintptr                 // asks the window's thread to register
)

// pointer turns an address Windows passed into a pointer, without go
// vet's complaint about converting a uintptr.
func pointer(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }

func handler() DropHandler {
	dropMu.Lock()
	defer dropMu.Unlock()
	return dropHandler
}

func setEffect(p uintptr, ok bool) {
	if p == 0 {
		return
	}
	e := (*uint32)(pointer(p))
	switch {
	case !ok:
		*e = dropEffectNone
	case *e&dropEffectCopy != 0:
		*e = dropEffectCopy
	case *e&dropEffectLink != 0:
		*e = dropEffectLink
	default:
		// Only a move is offered: the source would delete the files.
		*e = dropEffectNone
	}
}

// dataFiles returns the files an IDataObject carries.
func dataFiles(obj uintptr) []string {
	if obj == 0 {
		return nil
	}
	vtbl := *(**[12]uintptr)(pointer(obj))
	f := formatEtc{format: cfHDROP, aspect: dvaspContent, index: -1, tymed: tymedHGlobal}
	var m stgMedium
	if hr, _, _ := syscall.SyscallN(vtbl[3], obj, uintptr(unsafe.Pointer(&f)), uintptr(unsafe.Pointer(&m))); int32(hr) < 0 {
		return nil
	}
	defer procReleaseStgMedium.Call(uintptr(unsafe.Pointer(&m)))
	if m.tymed != tymedHGlobal || m.handle == 0 {
		return nil
	}
	n, _, _ := procDragQueryFileW.Call(m.handle, 0xFFFFFFFF, 0, 0)
	var out []string
	for i := range n {
		l, _, _ := procDragQueryFileW.Call(m.handle, i, 0, 0)
		if l == 0 {
			continue
		}
		buf := make([]uint16, l+1)
		procDragQueryFileW.Call(m.handle, i, uintptr(unsafe.Pointer(&buf[0])), l+1)
		out = append(out, syscall.UTF16ToString(buf))
	}
	return out
}

// hasFiles reports whether an IDataObject offers files.
func hasFiles(obj uintptr) bool {
	if obj == 0 {
		return false
	}
	vtbl := *(**[12]uintptr)(pointer(obj))
	f := formatEtc{format: cfHDROP, aspect: dvaspContent, index: -1, tymed: tymedHGlobal}
	hr, _, _ := syscall.SyscallN(vtbl[5], obj, uintptr(unsafe.Pointer(&f))) // QueryGetData
	return hr == sOK
}

type dropTarget struct{ vtbl *[7]uintptr }

var target = &dropTarget{vtbl: &[7]uintptr{
	syscall.NewCallback(func(this uintptr, riid *guid, ppv *uintptr) uintptr {
		if *riid == iidUnknown || *riid == iidDropTarget {
			*ppv = this
			return sOK
		}
		*ppv = 0
		return eNoInterface
	}),
	syscall.NewCallback(func(this uintptr) uintptr { return 1 }), // AddRef: it lives for good
	syscall.NewCallback(func(this uintptr) uintptr { return 1 }), // Release
	syscall.NewCallback(func(this, obj, keys, pt, effect uintptr) uintptr { // DragEnter
		dropFiles = hasFiles(obj)
		setEffect(effect, dropFiles)
		if h := handler(); dropFiles && h.Over != nil {
			h.Over(true)
		}
		return sOK
	}),
	syscall.NewCallback(func(this, keys, pt, effect uintptr) uintptr { // DragOver
		setEffect(effect, dropFiles)
		return sOK
	}),
	syscall.NewCallback(func(this uintptr) uintptr { // DragLeave
		if h := handler(); dropFiles && h.Over != nil {
			h.Over(false)
		}
		dropFiles = false
		return sOK
	}),
	syscall.NewCallback(func(this, obj, keys, pt, effect uintptr) uintptr { // Drop
		paths := dataFiles(obj)
		setEffect(effect, len(paths) > 0)
		h := handler()
		if dropFiles && h.Over != nil {
			h.Over(false)
		}
		dropFiles = false
		if len(paths) > 0 && h.Drop != nil {
			h.Drop(paths)
		}
		return sOK
	}),
}}

var subclassProc = syscall.NewCallback(func(hwnd, msg, wParam, lParam uintptr) uintptr {
	dropMu.Lock()
	old := dropOld[hwnd]
	dropMu.Unlock()
	switch {
	case msg == dropMsg && dropMsg != 0:
		// On the window's own thread at last.
		procOleInitialize.Call(0)
		procRegisterDragDrop.Call(hwnd, uintptr(unsafe.Pointer(target)))
		return 0
	case msg == wmNCDestroy:
		procRevokeDragDrop.Call(hwnd)
		procSetWindowLongPtrW.Call(hwnd, gwlpWndProc, old)
		dropMu.Lock()
		delete(dropOld, hwnd)
		dropMu.Unlock()
	}
	r, _, _ := procCallWindowProcW.Call(old, hwnd, msg, wParam, lParam)
	return r
})

// EnableDrop lets files be dropped on the window hwnd; h hears about
// them. Calling it again for the same window only replaces h.
func EnableDrop(hwnd uintptr, h DropHandler) {
	dropMu.Lock()
	dropHandler = h
	_, done := dropOld[hwnd]
	if dropMsg == 0 {
		name, _ := syscall.UTF16PtrFromString("WazzapClientsRegisterDrop")
		dropMsg, _, _ = procRegisterWindowMsgDnD.Call(uintptr(unsafe.Pointer(name)))
	}
	dropMu.Unlock()
	if done || hwnd == 0 || dropMsg == 0 {
		return
	}
	// The old procedure is known before the new one can run.
	old, _, _ := procGetWindowLongPtrW.Call(hwnd, gwlpWndProc)
	if old == 0 {
		return
	}
	dropMu.Lock()
	dropOld[hwnd] = old
	dropMu.Unlock()
	if r, _, _ := procSetWindowLongPtrW.Call(hwnd, gwlpWndProc, subclassProc); r == 0 {
		dropMu.Lock()
		delete(dropOld, hwnd)
		dropMu.Unlock()
		return
	}
	procPostMessageW.Call(hwnd, dropMsg, 0, 0)
}
