//go:build windows

package desktop

import (
	"fmt"
	"hash/fnv"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW         = user32.NewProc("RegisterClassExW")
	procCreateWindowExW          = user32.NewProc("CreateWindowExW")
	procDefWindowProcW           = user32.NewProc("DefWindowProcW")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procGetMessageW              = user32.NewProc("GetMessageW")
	procTranslateMessage         = user32.NewProc("TranslateMessage")
	procDispatchMessageW         = user32.NewProc("DispatchMessageW")
	procPostMessageW             = user32.NewProc("PostMessageW")
	procPostQuitMessage          = user32.NewProc("PostQuitMessage")
	procRegisterWindowMessageW   = user32.NewProc("RegisterWindowMessageW")
	procCreatePopupMenu          = user32.NewProc("CreatePopupMenu")
	procAppendMenuW              = user32.NewProc("AppendMenuW")
	procSetMenuDefaultItem       = user32.NewProc("SetMenuDefaultItem")
	procTrackPopupMenuEx         = user32.NewProc("TrackPopupMenuEx")
	procDestroyMenu              = user32.NewProc("DestroyMenu")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procFindWindowW              = user32.NewProc("FindWindowW")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procAllowSetForegroundWindow = user32.NewProc("AllowSetForegroundWindow")
	procGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	procCreateIconIndirect       = user32.NewProc("CreateIconIndirect")
	procCreateDIBSection         = gdi32.NewProc("CreateDIBSection")
	procCreateBitmap             = gdi32.NewProc("CreateBitmap")
	procDeleteObject             = gdi32.NewProc("DeleteObject")
	procShellNotifyIconW         = shell32.NewProc("Shell_NotifyIconW")
	procGetModuleHandleW         = kernel32.NewProc("GetModuleHandleW")
	procCreateMutexW             = kernel32.NewProc("CreateMutexW")
)

const (
	wmNull        = 0x0000
	wmDestroy     = 0x0002
	wmClose       = 0x0010
	wmContextMenu = 0x007b
	wmSetIcon     = 0x0080
	wmApp         = 0x8000

	wmTray  = wmApp + 1 // the icon's callback message
	wmShow  = wmApp + 2 // another instance started (see Lock)
	wmTip   = wmApp + 3 // SetTooltip
	ninSel  = 0x0400    // NIN_SELECT: the icon was clicked
	ninKey  = 0x0401    // NIN_KEYSELECT: chosen with the keyboard
	nimAdd  = 0
	nimMod  = 1
	nimDel  = 2
	nimVer  = 4
	nifMsg  = 0x01
	nifIcon = 0x02
	nifTip  = 0x04
	nifShow = 0x80 // NIF_SHOWTIP: version 4 shows tooltips only with it
	nver4   = 4

	mfString       = 0x0000
	mfSeparator    = 0x0800
	tpmRightButton = 0x0002
	tpmRightAlign  = 0x0008
	tpmBottomAlign = 0x0020
	tpmNoNotify    = 0x0080
	tpmReturnCmd   = 0x0100

	smCXIcon            = 11
	smCXSmIcon          = 49
	smMenuDropAlignment = 40
	iconSmall, iconBig  = 0, 1

	errorAlreadyExists = 183
)

type wndClassEx struct {
	size, style                uint32
	wndProc                    uintptr
	clsExtra, wndExtra         int32
	instance, icon, cursor, bg uintptr
	menuName, className        *uint16
	iconSm                     uintptr
}

type msg struct {
	hwnd           uintptr
	message        uint32
	wParam, lParam uintptr
	time           uint32
	x, y           int32
	private        uint32
}

type notifyIconData struct {
	size             uint32
	wnd              uintptr
	id, flags        uint32
	callbackMessage  uint32
	icon             uintptr
	tip              [128]uint16
	state, stateMask uint32
	info             [256]uint16
	version          uint32
	infoTitle        [64]uint16
	infoFlags        uint32
	guidItem         [16]byte
	balloonIcon      uintptr
}

// instanceKey names the mutex and tray window of the instance using a
// data directory.
var instanceKey string

func className() string { return "WazzapClientsTray-" + instanceKey }

// Lock makes this process the only instance of the app using the data
// directory dir. When another instance has it, Lock asks that one to show
// its window and returns false.
func Lock(dir string) bool {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	h := fnv.New64a()
	h.Write([]byte(strings.ToLower(filepath.Clean(dir))))
	instanceKey = fmt.Sprintf("%016x", h.Sum64())
	name, _ := syscall.UTF16PtrFromString(`Local\WazzapClients-` + instanceKey)
	// The handle stays open for the life of the process.
	m, _, err := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if m == 0 || err != syscall.Errno(errorAlreadyExists) {
		return true
	}
	// The other instance may still be starting: wait for its tray window.
	cls, _ := syscall.UTF16PtrFromString(className())
	for range 50 {
		if w, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(cls)), 0); w != 0 {
			// Windows lets only the foreground app bring a window to
			// the front; launching this one made it the foreground app.
			var pid uint32
			procGetWindowThreadProcessId.Call(w, uintptr(unsafe.Pointer(&pid)))
			procAllowSetForegroundWindow.Call(uintptr(pid))
			procPostMessageW.Call(w, wmShow, 0, 0)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

var tray struct {
	sync.Mutex
	t    Tray
	hwnd uintptr
	tip  string
	done chan struct{}
	// taskbarCreated is broadcast when Explorer (re)starts, after which
	// the icon must be added again.
	taskbarCreated uintptr
	icon           uintptr
}

// TrayStart shows the tray icon, and serves Lock's requests to show the
// window, until TrayStop.
func TrayStart(t Tray) error {
	if shell32.Load() != nil {
		return ErrUnsupported
	}
	tray.t, tray.tip = t, t.Name
	errc := make(chan error, 1)
	tray.done = make(chan struct{})
	go trayLoop(errc)
	return <-errc
}

// TrayStop removes the tray icon.
func TrayStop() {
	tray.Lock()
	hwnd := tray.hwnd
	tray.Unlock()
	if hwnd != 0 {
		procPostMessageW.Call(hwnd, wmClose, 0, 0)
		<-tray.done
	}
}

// SetTooltip changes the text shown when the pointer rests on the icon.
func SetTooltip(s string) {
	tray.Lock()
	changed := tray.tip != s
	tray.tip = s
	hwnd := tray.hwnd
	tray.Unlock()
	if changed && hwnd != 0 {
		procPostMessageW.Call(hwnd, wmTip, 0, 0)
	}
}

func trayLoop(errc chan<- error) {
	runtime.LockOSThread()
	defer close(tray.done)
	inst, _, _ := procGetModuleHandleW.Call(0)
	cls, _ := syscall.UTF16PtrFromString(className())
	wc := wndClassEx{
		wndProc:   syscall.NewCallback(trayProc),
		instance:  inst,
		className: cls,
	}
	wc.size = uint32(unsafe.Sizeof(wc))
	if a, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); a == 0 {
		errc <- fmt.Errorf("desktop: register tray window class: %w", err)
		return
	}
	title, _ := syscall.UTF16PtrFromString(tray.t.Name)
	// A hidden top-level window: message-only windows don't get the
	// TaskbarCreated broadcast.
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0, 0, 0, inst, 0)
	if hwnd == 0 {
		errc <- fmt.Errorf("desktop: create tray window: %w", err)
		return
	}
	tc, _ := syscall.UTF16PtrFromString("TaskbarCreated")
	tray.taskbarCreated, _, _ = procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(tc)))
	allowDarkMenus()
	px, _, _ := procGetSystemMetrics.Call(smCXSmIcon)
	tray.icon = createIcon(tray.t.Icon(int(px)))
	tray.Lock()
	tray.hwnd = hwnd
	tray.Unlock()
	if !addIcon(hwnd) {
		procDestroyWindow.Call(hwnd)
		errc <- fmt.Errorf("desktop: Shell_NotifyIcon failed")
		return
	}
	errc <- nil
	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func iconData(hwnd uintptr) *notifyIconData {
	d := &notifyIconData{wnd: hwnd, id: 1, flags: nifMsg | nifIcon | nifTip | nifShow,
		callbackMessage: wmTray, icon: tray.icon}
	d.size = uint32(unsafe.Sizeof(*d))
	tray.Lock()
	tip, _ := syscall.UTF16FromString(tray.tip)
	tray.Unlock()
	copy(d.tip[:len(d.tip)-1], tip)
	return d
}

func addIcon(hwnd uintptr) bool {
	d := iconData(hwnd)
	if r, _, _ := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(d))); r == 0 {
		return false
	}
	d.version = nver4
	procShellNotifyIconW.Call(nimVer, uintptr(unsafe.Pointer(d)))
	return true
}

func trayProc(hwnd, m, wParam, lParam uintptr) uintptr {
	switch m {
	case wmTray:
		switch lParam & 0xffff {
		case ninSel, ninKey:
			tray.t.Open()
		case wmContextMenu:
			// Version 4 sends the menu's anchor in wParam.
			showMenu(hwnd, int32(int16(wParam&0xffff)), int32(int16(wParam>>16&0xffff)))
		}
		return 0
	case wmShow:
		tray.t.Open()
		return 0
	case wmTip:
		procShellNotifyIconW.Call(nimMod, uintptr(unsafe.Pointer(iconData(hwnd))))
		return 0
	case wmDestroy:
		procShellNotifyIconW.Call(nimDel, uintptr(unsafe.Pointer(iconData(hwnd))))
		tray.Lock()
		tray.hwnd = 0
		tray.Unlock()
		procPostQuitMessage.Call(0)
		return 0
	}
	if m == tray.taskbarCreated && m != 0 {
		addIcon(hwnd)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, m, wParam, lParam)
	return r
}

func showMenu(hwnd uintptr, x, y int32) {
	const cmdOpen, cmdQuit = 1, 2
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	open, _ := syscall.UTF16PtrFromString("Open " + tray.t.Name)
	quit, _ := syscall.UTF16PtrFromString("Quit " + tray.t.Name)
	procAppendMenuW.Call(menu, mfString, cmdOpen, uintptr(unsafe.Pointer(open)))
	procAppendMenuW.Call(menu, mfSeparator, 0, 0)
	procAppendMenuW.Call(menu, mfString, cmdQuit, uintptr(unsafe.Pointer(quit)))
	procSetMenuDefaultItem.Call(menu, cmdOpen, 0)
	// Without being the foreground window, the menu wouldn't close when
	// clicking elsewhere.
	procSetForegroundWindow.Call(hwnd)
	flags := uintptr(tpmRightButton | tpmBottomAlign | tpmNoNotify | tpmReturnCmd)
	if r, _, _ := procGetSystemMetrics.Call(smMenuDropAlignment); r != 0 {
		flags |= tpmRightAlign
	}
	cmd, _, _ := procTrackPopupMenuEx.Call(menu, flags, uintptr(x), uintptr(y), hwnd, 0)
	procPostMessageW.Call(hwnd, wmNull, 0, 0)
	switch cmd {
	case cmdOpen:
		tray.t.Open()
	case cmdQuit:
		tray.t.Quit()
	}
}

// allowDarkMenus lets the tray menu follow the system's dark mode, as
// Explorer's own menus do. uxtheme has no documented call for it: ordinal
// 135 is SetPreferredAppMode since Windows 10 1903 (AllowDarkModeForApp
// in 1809), and 1 is "allow dark" for both.
func allowDarkMenus() {
	if v := windows.RtlGetVersion(); v.MajorVersion < 10 || v.BuildNumber < 17763 {
		return
	}
	h, err := windows.LoadLibrary("uxtheme.dll")
	if err != nil {
		return
	}
	if p, err := windows.GetProcAddressByOrdinal(h, 135); err == nil {
		syscall.SyscallN(p, 1)
	}
	if p, err := windows.GetProcAddressByOrdinal(h, 136); err == nil { // FlushMenuThemes
		syscall.SyscallN(p)
	}
}

// SetWindowIcon gives the window hwnd the icon draw draws, in the title
// bar size and the taskbar size. It doesn't wait for the window: Gio's
// window thread waits for the UI goroutine while it delivers an event, so
// sending to it from there would hang both.
func SetWindowIcon(hwnd uintptr, draw func(px int) *image.RGBA) {
	for _, s := range []struct{ which, metric uintptr }{{iconSmall, smCXSmIcon}, {iconBig, smCXIcon}} {
		px, _, _ := procGetSystemMetrics.Call(s.metric)
		windowIcons.Lock()
		h, ok := windowIcons.m[int(px)]
		if !ok {
			h = createIcon(draw(int(px)))
			windowIcons.m[int(px)] = h
		}
		windowIcons.Unlock()
		if h != 0 {
			procPostMessageW.Call(hwnd, wmSetIcon, s.which, h)
		}
	}
}

// windowIcons keeps the window icons by size: a window doesn't copy the
// icon it's given, and new windows reuse them.
var windowIcons = struct {
	sync.Mutex
	m map[int]uintptr
}{m: map[int]uintptr{}}

type bitmapInfoHeader struct {
	size                       uint32
	width, height              int32
	planes, bitCount           uint16
	compression, sizeImage     uint32
	xPelsPerMeter, yPelsPerMtr int32
	clrUsed, clrImportant      uint32
}

type iconInfo struct {
	isIcon     int32
	xHot, yHot uint32
	mask, bits uintptr
}

// createIcon makes an HICON of img.
func createIcon(img *image.RGBA) uintptr {
	if img == nil {
		return 0
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	bi := bitmapInfoHeader{width: int32(w), height: -int32(h), planes: 1, bitCount: 32}
	bi.size = uint32(unsafe.Sizeof(bi))
	var bits unsafe.Pointer
	color, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if color == 0 {
		return 0
	}
	defer procDeleteObject.Call(color)
	// Icons take BGRA with straight alpha; image.RGBA is premultiplied.
	dst := unsafe.Slice((*byte)(bits), w*h*4)
	for y := range h {
		for x := range w {
			s := img.Pix[img.PixOffset(x, y):]
			d := dst[(y*w+x)*4:]
			a := uint32(s[3])
			if a == 0 {
				d[0], d[1], d[2], d[3] = 0, 0, 0, 0
				continue
			}
			d[0] = byte(uint32(s[2]) * 255 / a)
			d[1] = byte(uint32(s[1]) * 255 / a)
			d[2] = byte(uint32(s[0]) * 255 / a)
			d[3] = byte(a)
		}
	}
	mask, _, _ := procCreateBitmap.Call(uintptr(w), uintptr(h), 1, 1, 0)
	if mask == 0 {
		return 0
	}
	defer procDeleteObject.Call(mask)
	ii := iconInfo{isIcon: 1, mask: mask, bits: color}
	icon, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	return icon
}

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// StartAtLogin reports whether the app starts when the user logs in.
func StartAtLogin() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue("WazzapClients")
	return err == nil
}

// SetStartAtLogin makes the app start at login with args, or not.
func SetStartAtLogin(on bool, args []string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue("WazzapClients"); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := syscall.EscapeArg(exe)
	for _, a := range args {
		cmd += " " + syscall.EscapeArg(a)
	}
	return k.SetStringValue("WazzapClients", cmd)
}

// WaitExit waits up to d for the process pid to exit: a restarted app
// waits for the instance that started it to let go of Lock.
func WaitExit(pid int, d time.Duration) {
	p, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return // gone already
	}
	defer windows.CloseHandle(p)
	windows.WaitForSingleObject(p, uint32(d/time.Millisecond))
}
