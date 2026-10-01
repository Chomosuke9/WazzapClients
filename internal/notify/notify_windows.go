//go:build windows

package notify

// Toasts go through the Windows Runtime (Windows.UI.Notifications), called
// through syscall without cgo, as internal/video calls COM. An app without
// a package identity can show toasts once its AppUserModelID is in the
// registry (HKCU\Software\Classes\AppUserModelId\<id>) with a display name
// and icon. Clicks reach it through COM: the registration names a class
// (CustomActivator) that the app serves with CoRegisterClassObject while it
// runs, and that Windows starts it to serve (LocalServer32) when it
// doesn't.
//
// Every pointer argument is converted in the syscall.SyscallN argument
// list itself, which keeps it alive and in place for the call.

import (
	"errors"
	"fmt"
	"hash/fnv"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

var (
	combase = syscall.NewLazyDLL("combase.dll")
	ole32   = syscall.NewLazyDLL("ole32.dll")

	procRoInitialize           = combase.NewProc("RoInitialize")
	procRoActivateInstance     = combase.NewProc("RoActivateInstance")
	procRoGetActivationFactory = combase.NewProc("RoGetActivationFactory")
	procWindowsCreateString    = combase.NewProc("WindowsCreateString")
	procWindowsDeleteString    = combase.NewProc("WindowsDeleteString")
	procCoRegisterClassObject  = ole32.NewProc("CoRegisterClassObject")
	procCoRevokeClassObject    = ole32.NewProc("CoRevokeClassObject")
)

type guid struct {
	d1     uint32
	d2, d3 uint16
	d4     [8]byte
}

func (g guid) String() string {
	return fmt.Sprintf("{%08X-%04X-%04X-%02X%02X-%02X%02X%02X%02X%02X%02X}", g.d1, g.d2, g.d3,
		g.d4[0], g.d4[1], g.d4[2], g.d4[3], g.d4[4], g.d4[5], g.d4[6], g.d4[7])
}

var (
	iidIUnknown                        = guid{0x00000000, 0x0000, 0x0000, [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIClassFactory                   = guid{0x00000001, 0x0000, 0x0000, [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidINotificationActivationCallback = guid{0x53e31837, 0x6600, 0x4a81, [8]byte{0x93, 0x95, 0x75, 0xcf, 0xfe, 0x74, 0x6f, 0x94}}
	iidIXmlDocument                    = guid{0xf7f3a506, 0x1e87, 0x42d6, [8]byte{0xbc, 0xfb, 0xb8, 0xc8, 0x09, 0xfa, 0x54, 0x94}}
	iidIXmlDocumentIO                  = guid{0x6cd0e74e, 0xee65, 0x4489, [8]byte{0x9e, 0xbf, 0xca, 0x43, 0xe8, 0x7b, 0xa6, 0x37}}
	iidIToastNotificationFactory       = guid{0x04124b20, 0x82c6, 0x4229, [8]byte{0xb1, 0x09, 0xfd, 0x9e, 0xd4, 0x66, 0x2b, 0x53}}
	iidIToastNotification2             = guid{0x9dfb9fd1, 0x143a, 0x490e, [8]byte{0x90, 0xbf, 0xb9, 0xfb, 0xa7, 0x13, 0x2d, 0xe7}}
	iidIToastNotificationMgrStatics5   = guid{0xd6f5f569, 0xd40d, 0x407c, [8]byte{0x89, 0x89, 0x88, 0xca, 0xb4, 0x2c, 0xfd, 0x14}}

	// clsidActivator is the COM class Windows calls when one of the
	// app's toasts is clicked. It's WazzapClients' own.
	clsidActivator = guid{0xec56f81c, 0x5d4a, 0x4d8c, [8]byte{0x80, 0x44, 0x76, 0x1d, 0xda, 0x20, 0xed, 0xc8}}
)

// Vtable slots, from windows.ui.notifications.idl and windows.data.xml.dom.idl.
// IInspectable's own methods take slots 0 to 5.
const (
	vQueryInterface = 0
	vRelease        = 2

	vLoadXml                 = 6 // IXmlDocumentIO
	vCreateToastNotification = 6 // IToastNotificationFactory
	vPutTag                  = 6 // IToastNotification2
	vPutGroup                = 8
	vGetDefault              = 6 // IToastNotificationManagerStatics5
	vCreateNotifierWithID    = 7 // IToastNotificationManagerForUser
	vGetHistory              = 8
	vShow                    = 6 // IToastNotifier
	vRemoveGroupedTagWithID  = 8 // IToastNotificationHistory

	roInitMultithreaded = 1
	clsctxLocalServer   = 4
	regclsMultipleUse   = 1
	eNoInterface        = 0x80004002
	classENoAggregation = 0x80040110
)

// group holds all of the app's toasts in the notification center; a
// toast's tag (from its ID) tells them apart.
const group = "chats"

// comObj is a COM interface pointer: a pointer to its vtable.
type comObj struct{ vtbl *[64]uintptr }

func (o *comObj) release() {
	if o != nil {
		syscall.SyscallN(o.vtbl[vRelease], uintptr(unsafe.Pointer(o)))
	}
}

func (o *comObj) query(iid *guid) (*comObj, error) {
	var out *comObj
	hr, _, _ := syscall.SyscallN(o.vtbl[vQueryInterface], uintptr(unsafe.Pointer(o)),
		uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if failed(hr) {
		return nil, hresult(hr)
	}
	return out, nil
}

func failed(hr uintptr) bool { return int32(hr) < 0 }

type hresult uintptr

func (h hresult) Error() string { return fmt.Sprintf("notify: HRESULT 0x%08X", uint32(h)) }

// hstring is a Windows Runtime string.
type hstring uintptr

func newHString(s string) (hstring, error) {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var h hstring
	hr, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h)))
	if failed(hr) {
		return 0, hresult(hr)
	}
	return h, nil
}

func (h hstring) free() { procWindowsDeleteString.Call(uintptr(h)) }

// factory returns a runtime class's activation factory interface iid.
func factory(class string, iid *guid) (*comObj, error) {
	name, err := newHString(class)
	if err != nil {
		return nil, err
	}
	defer name.free()
	var out *comObj
	hr, _, _ := procRoGetActivationFactory.Call(uintptr(name), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if failed(hr) {
		return nil, hresult(hr)
	}
	return out, nil
}

// callStrings calls method slot of o with WinRT strings made of args.
func callStrings(o *comObj, slot int, args ...string) error {
	hs := make([]uintptr, 0, len(args)+1)
	hs = append(hs, uintptr(unsafe.Pointer(o)))
	for _, a := range args {
		h, err := newHString(a)
		if err != nil {
			return err
		}
		defer h.free()
		hs = append(hs, uintptr(h))
	}
	if hr, _, _ := syscall.SyscallN(o.vtbl[slot], hs...); failed(hr) {
		return hresult(hr)
	}
	return nil
}

var (
	opts  Options
	ready bool // Init succeeded
	jobs  = make(chan func(*toaster), 16)
	start sync.Once
	// ErrUnsupported reports a Windows without toasts for desktop apps.
	ErrUnsupported = errors.New("notify: notifications aren't supported")
)

// Init registers the app for notifications and starts serving their
// clicks. Call it once, before Show.
func Init(o Options) error {
	if combase.Load() != nil || ole32.Load() != nil || procRoGetActivationFactory.Find() != nil {
		return ErrUnsupported
	}
	err := ErrUnsupported
	start.Do(func() {
		opts = o
		errc := make(chan error, 1)
		go run(errc)
		err = <-errc
		ready = err == nil
	})
	return err
}

// run owns the Windows Runtime objects, on a thread of their own.
func run(errc chan<- error) {
	runtime.LockOSThread()
	// RPC_E_CHANGED_MODE can't happen on a fresh thread; S_FALSE (already
	// initialized) is fine.
	if hr, _, _ := procRoInitialize.Call(roInitMultithreaded); failed(hr) {
		errc <- hresult(hr)
		return
	}
	if err := register(); err != nil {
		errc <- err
		return
	}
	t := new(toaster)
	if err := t.init(); err != nil {
		revoke()
		errc <- err
		return
	}
	errc <- nil
	go pruneImages(opts.Dir)
	for job := range jobs {
		job(t)
	}
}

// Show shows n, replacing the notification with the same ID. It returns
// at once; failures are dropped.
func Show(n Notification) {
	if !ready {
		return
	}
	select {
	case jobs <- func(t *toaster) { t.show(n) }:
	default: // a backlog of toasts: this one would be stale anyway
	}
}

// Remove takes the notification with ID id off the screen and out of the
// notification center.
func Remove(id string) {
	if !ready {
		return
	}
	select {
	case jobs <- func(t *toaster) { t.remove(id) }:
	default:
	}
}

// Close stops serving notification clicks; Windows starts the app again
// (Options.Command) for the next one.
func Close() {
	if !ready {
		return
	}
	done := make(chan struct{})
	jobs <- func(*toaster) { revoke(); close(done) }
	<-done
}

// toaster holds the objects every toast needs.
type toaster struct {
	factory  *comObj // IToastNotificationFactory
	notifier *comObj // IToastNotifier
	history  *comObj // IToastNotificationHistory
}

func (t *toaster) init() error {
	var err error
	if t.factory, err = factory("Windows.UI.Notifications.ToastNotification", &iidIToastNotificationFactory); err != nil {
		return err
	}
	statics, err := factory("Windows.UI.Notifications.ToastNotificationManager", &iidIToastNotificationMgrStatics5)
	if err != nil {
		return err
	}
	defer statics.release()
	var mgr *comObj // IToastNotificationManagerForUser
	if hr, _, _ := syscall.SyscallN(statics.vtbl[vGetDefault], uintptr(unsafe.Pointer(statics)),
		uintptr(unsafe.Pointer(&mgr))); failed(hr) {
		return hresult(hr)
	}
	defer mgr.release()
	appID, err := newHString(opts.AppID)
	if err != nil {
		return err
	}
	defer appID.free()
	if hr, _, _ := syscall.SyscallN(mgr.vtbl[vCreateNotifierWithID], uintptr(unsafe.Pointer(mgr)), uintptr(appID),
		uintptr(unsafe.Pointer(&t.notifier))); failed(hr) {
		return hresult(hr)
	}
	if hr, _, _ := syscall.SyscallN(mgr.vtbl[vGetHistory], uintptr(unsafe.Pointer(mgr)),
		uintptr(unsafe.Pointer(&t.history))); failed(hr) {
		t.history = nil // removing toasts is a nicety
	}
	return nil
}

func (t *toaster) show(n Notification) error {
	doc, err := xmlDocument(toastXML(n, imageFile(opts.Dir, n.Image)))
	if err != nil {
		return err
	}
	defer doc.release()
	var toast *comObj // IToastNotification
	if hr, _, _ := syscall.SyscallN(t.factory.vtbl[vCreateToastNotification], uintptr(unsafe.Pointer(t.factory)),
		uintptr(unsafe.Pointer(doc)), uintptr(unsafe.Pointer(&toast))); failed(hr) {
		return hresult(hr)
	}
	defer toast.release()
	if t2, err := toast.query(&iidIToastNotification2); err == nil {
		callStrings(t2, vPutTag, tag(n.ID))
		callStrings(t2, vPutGroup, group)
		t2.release()
	}
	if hr, _, _ := syscall.SyscallN(t.notifier.vtbl[vShow], uintptr(unsafe.Pointer(t.notifier)),
		uintptr(unsafe.Pointer(toast))); failed(hr) {
		return hresult(hr)
	}
	return nil
}

func (t *toaster) remove(id string) {
	if t.history != nil {
		callStrings(t.history, vRemoveGroupedTagWithID, tag(id), group, opts.AppID)
	}
}

// xmlDocument parses a toast's XML into an IXmlDocument.
func xmlDocument(xml string) (*comObj, error) {
	class, err := newHString("Windows.Data.Xml.Dom.XmlDocument")
	if err != nil {
		return nil, err
	}
	defer class.free()
	var inst *comObj
	if hr, _, _ := procRoActivateInstance.Call(uintptr(class), uintptr(unsafe.Pointer(&inst))); failed(hr) {
		return nil, hresult(hr)
	}
	defer inst.release()
	io, err := inst.query(&iidIXmlDocumentIO)
	if err != nil {
		return nil, err
	}
	defer io.release()
	if err := callStrings(io, vLoadXml, xml); err != nil {
		return nil, err
	}
	return inst.query(&iidIXmlDocument)
}

// tag is a toast's tag: Windows allows at most 64 characters.
func tag(id string) string {
	h := fnv.New64a()
	h.Write([]byte(id))
	return fmt.Sprintf("%016x", h.Sum64())
}

// toastXML describes a toast; see
// https://learn.microsoft.com/windows/apps/design/shell/tiles-and-notifications/adaptive-interactive-toasts
func toastXML(n Notification, image string) string {
	var b strings.Builder
	b.WriteString(`<toast launch="`)
	b.WriteString(esc(args(n.ID, "")))
	b.WriteByte('"')
	if !n.Time.IsZero() {
		b.WriteString(` displayTimestamp="` + n.Time.UTC().Format("2006-01-02T15:04:05Z") + `"`)
	}
	b.WriteString(`><visual><binding template="ToastGeneric">`)
	b.WriteString(`<text hint-maxLines="1">` + esc(n.Title) + `</text>`)
	if n.Body != "" {
		b.WriteString(`<text>` + esc(n.Body) + `</text>`)
	}
	if n.Footer != "" {
		b.WriteString(`<text placement="attribution">` + esc(n.Footer) + `</text>`)
	}
	if image != "" {
		b.WriteString(`<image placement="appLogoOverride" hint-crop="circle" src="` + esc(image) + `"/>`)
	}
	b.WriteString(`</binding></visual>`)
	if n.Reply {
		b.WriteString(`<actions><input id="reply" type="text" placeHolderContent="Type a reply"/>`)
		b.WriteString(`<action content="Reply" activationType="background" arguments="` + esc(args(n.ID, "reply")) + `"/>`)
		b.WriteString(`<action content="Mark as read" activationType="background" arguments="` + esc(args(n.ID, "read")) + `"/>`)
		b.WriteString(`</actions>`)
	}
	if n.Silent {
		b.WriteString(`<audio silent="true"/>`)
	} else {
		b.WriteString(`<audio src="ms-winsoundevent:Notification.IM"/>`)
	}
	b.WriteString(`</toast>`)
	return b.String()
}

// esc escapes text for XML text and attribute values. Characters XML
// can't hold at all (most control characters) are dropped.
func esc(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		case r == '"':
			b.WriteString("&quot;")
		case r == '\n':
			b.WriteString("&#10;")
		case r < 0x20 || r == 0xfffe || r == 0xffff:
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// args encodes what a click on a toast or one of its buttons is about.
func args(id, action string) string {
	v := url.Values{"id": {id}}
	if action != "" {
		v.Set("do", action)
	}
	return v.Encode()
}

func parseArgs(s string, input map[string]string) Activation {
	v, _ := url.ParseQuery(s)
	a := Activation{ID: v.Get("id")}
	switch v.Get("do") {
	case "reply":
		a.Action, a.Text = Reply, input["reply"]
	case "read":
		a.Action = MarkRead
	}
	return a
}

// register writes the app's notification settings to the registry and
// serves the activator class.
func register() error {
	base := `Software\Classes\AppUserModelId\` + opts.AppID
	icon := ""
	if p := imageFile(filepath.Join(opts.Dir, "app"), opts.Icon); p != "" {
		icon = p
	}
	cmd := ""
	for i, a := range opts.Command {
		if i > 0 {
			cmd += " "
		}
		cmd += syscall.EscapeArg(a)
	}
	for _, v := range []struct{ key, name, value string }{
		{base, "DisplayName", opts.Name},
		{base, "IconUri", icon},
		{base, "CustomActivator", clsidActivator.String()},
		{`Software\Classes\CLSID\` + clsidActivator.String() + `\LocalServer32`, "", cmd},
	} {
		if v.value == "" {
			continue
		}
		if err := setString(v.key, v.name, v.value); err != nil {
			return err
		}
	}
	var cookie uint32
	if hr, _, _ := procCoRegisterClassObject.Call(uintptr(unsafe.Pointer(&clsidActivator)), uintptr(unsafe.Pointer(&classFactory)),
		clsctxLocalServer, regclsMultipleUse, uintptr(unsafe.Pointer(&cookie))); failed(hr) {
		return hresult(hr)
	}
	classCookie = cookie
	return nil
}

var classCookie uint32

func revoke() {
	if classCookie != 0 {
		procCoRevokeClassObject.Call(uintptr(classCookie))
		classCookie = 0
	}
}

// setString sets a registry value under HKEY_CURRENT_USER, unless it has
// that value already.
func setString(path, name, value string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if old, _, err := k.GetStringValue(name); err == nil && old == value {
		return nil
	}
	return k.SetStringValue(name, value)
}

// The activator's COM objects: an IClassFactory that hands out the one
// INotificationActivationCallback. Both are static, so reference counts
// don't matter. Windows calls them on its own threads.
type staticObj struct{ vtbl *[5]uintptr }

var (
	classFactory staticObj
	activator    staticObj
)

func init() {
	addRef := syscall.NewCallback(func(this uintptr) uintptr { return 1 })
	queryFor := func(iid *guid) uintptr {
		return syscall.NewCallback(func(this uintptr, riid *guid, ppv *uintptr) uintptr {
			if *riid == iidIUnknown || *riid == *iid {
				*ppv = this
				return 0
			}
			*ppv = 0
			return eNoInterface
		})
	}
	classFactory.vtbl = &[5]uintptr{
		queryFor(&iidIClassFactory),
		addRef,
		addRef, // Release
		// CreateInstance
		syscall.NewCallback(func(this, outer uintptr, riid *guid, ppv *uintptr) uintptr {
			*ppv = 0
			if outer != 0 {
				return classENoAggregation
			}
			if *riid != iidIUnknown && *riid != iidINotificationActivationCallback {
				return eNoInterface
			}
			*ppv = uintptr(unsafe.Pointer(&activator))
			return 0
		}),
		syscall.NewCallback(func(this, lock uintptr) uintptr { return 0 }), // LockServer
	}
	activator.vtbl = &[5]uintptr{
		queryFor(&iidINotificationActivationCallback),
		addRef,
		addRef,
		// Activate(appUserModelId, invokedArgs, data, count)
		syscall.NewCallback(func(this uintptr, appID, invoked *uint16, data unsafe.Pointer, count uintptr) uintptr {
			type inputData struct{ key, value *uint16 }
			input := map[string]string{}
			if data != nil {
				for _, d := range unsafe.Slice((*inputData)(data), uint32(count)) {
					input[utf16PtrToString(d.key)] = utf16PtrToString(d.value)
				}
			}
			a := parseArgs(utf16PtrToString(invoked), input)
			if f := opts.Activate; f != nil {
				go f(a)
			}
			return 0
		}),
	}
}

func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	n := 0
	for ptr := unsafe.Pointer(p); *(*uint16)(ptr) != 0; n++ {
		ptr = unsafe.Add(ptr, 2)
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}
