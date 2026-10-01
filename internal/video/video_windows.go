//go:build windows

package video

// The Windows backend drives Media Foundation's Media Engine, the engine
// behind the HTML <video> element in Edge, in "frame server" mode: it
// decodes and plays the audio itself, and hands over each video frame on
// request (TransferVideoFrame).
//
// With a Direct3D 11 device it decodes on the GPU, which keeps the
// decoder's buffers in video memory and takes little CPU; frames go into a
// texture and are copied out through a staging texture. Without one (no GPU
// driver, some remote desktops) it decodes in software and frames go into a
// WIC bitmap.
//
// COM is called through syscall, without cgo. Every pointer argument is
// converted in the syscall.SyscallN argument list itself, which keeps it
// alive and in place for the call.

import (
	"errors"
	"image"
	"math"
	"net/url"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	ole32    = syscall.NewLazyDLL("ole32.dll")
	oleaut32 = syscall.NewLazyDLL("oleaut32.dll")
	mfplat   = syscall.NewLazyDLL("mfplat.dll")
	d3d11    = syscall.NewLazyDLL("d3d11.dll")

	procCoInitializeEx     = ole32.NewProc("CoInitializeEx")
	procCoUninitialize     = ole32.NewProc("CoUninitialize")
	procCoCreateInstance   = ole32.NewProc("CoCreateInstance")
	procSysAllocString     = oleaut32.NewProc("SysAllocString")
	procSysFreeString      = oleaut32.NewProc("SysFreeString")
	procMFStartup          = mfplat.NewProc("MFStartup")
	procMFShutdown         = mfplat.NewProc("MFShutdown")
	procMFCreateAttributes = mfplat.NewProc("MFCreateAttributes")
	procMFCreateDXGIDevMgr = mfplat.NewProc("MFCreateDXGIDeviceManager")
	procD3D11CreateDevice  = d3d11.NewProc("D3D11CreateDevice")
)

type guid struct {
	d1     uint32
	d2, d3 uint16
	d4     [8]byte
}

// From mfmediaengine.h, wincodec.h and d3d10.h.
var (
	iidIUnknown                    = guid{0x00000000, 0x0000, 0x0000, [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	clsidMFMediaEngineClassFactory = guid{0xb44392da, 0x499b, 0x446b, [8]byte{0xa4, 0xcb, 0x00, 0x5f, 0xea, 0xd0, 0xe6, 0xd5}}
	iidIMFMediaEngineClassFactory  = guid{0x4d645ace, 0x26aa, 0x4688, [8]byte{0x9b, 0xe1, 0xdf, 0x35, 0x16, 0x99, 0x0b, 0x93}}
	iidIMFMediaEngineNotify        = guid{0xfee7c112, 0xe776, 0x42b5, [8]byte{0x9b, 0xbf, 0x00, 0x48, 0x52, 0x4e, 0x2b, 0xd5}}
	iidIMFMediaEngineEx            = guid{0x83015ead, 0xb1e6, 0x40d0, [8]byte{0xa9, 0x8a, 0x37, 0x14, 0x5f, 0xfe, 0x1a, 0xd1}}
	mfMediaEngineCallback          = guid{0xc60381b8, 0x83a4, 0x41f8, [8]byte{0xa3, 0xd0, 0xde, 0x05, 0x07, 0x68, 0x49, 0xa9}}
	mfMediaEngineVideoOutputFormat = guid{0x5066893c, 0x8cf9, 0x42bc, [8]byte{0x8b, 0x8a, 0x47, 0x22, 0x12, 0xe5, 0x27, 0x26}}
	mfMediaEngineDXGIManager       = guid{0x065702da, 0x1094, 0x486d, [8]byte{0x86, 0x17, 0xee, 0x7c, 0xc4, 0xee, 0x46, 0x48}}
	clsidWICImagingFactory         = guid{0xcacaf262, 0x9370, 0x4615, [8]byte{0xa1, 0x3b, 0x9f, 0x55, 0x39, 0xda, 0x4c, 0x0a}}
	iidIWICImagingFactory          = guid{0xec5ec8a9, 0xc395, 0x4314, [8]byte{0x9c, 0x77, 0x54, 0xd7, 0xa9, 0x35, 0xff, 0x70}}
	guidWICPixelFormat32bppBGRA    = guid{0x6fddc324, 0x4e03, 0x4bfe, [8]byte{0xb1, 0x85, 0x3d, 0x77, 0x76, 0x8d, 0xc9, 0x0f}}
	iidID3D10Multithread           = guid{0x9b7e4e00, 0x342c, 0x4106, [8]byte{0xa1, 0x9f, 0x4f, 0x27, 0x04, 0xf6, 0x89, 0xf0}}

	errPlay = errors.New("video: this video can't play")
)

const (
	mfVersion            = 0x0002<<16 | 0x0070
	coinitMultithreaded  = 0x0
	clsctxInprocServer   = 0x1
	dxgiFormatB8G8R8A8   = 87
	wicBitmapCacheOnLoad = 0x2
	eNoInterface         = 0x80004002
	sFalse               = 1
	haveMetadata         = 1 // MF_MEDIA_ENGINE_READY_HAVE_METADATA
	seekModeApproximate  = 1
	eventError           = 5 // MF_MEDIA_ENGINE_EVENT_ERROR
	pollPlaying          = 8 * time.Millisecond
	pollIdle             = 40 * time.Millisecond

	d3dDriverTypeHardware = 1
	d3dCreateBGRASupport  = 0x20
	d3dCreateVideoSupport = 0x800
	d3dSDKVersion         = 7
	d3dUsageStaging       = 3
	d3dBindRenderTarget   = 0x20
	d3dCPUAccessRead      = 0x20000
	d3dMapRead            = 1
)

// Vtable slots, checked against mfmediaengine.h, mfobjects.h, wincodec.h,
// d3d11.h and d3d10.h.
const (
	vQueryInterface = 0
	vRelease        = 2

	// IMFMediaEngine and IMFMediaEngineEx
	vSetSource          = 6
	vGetReadyState      = 14
	vGetCurrentTime     = 16
	vSetCurrentTime     = 17
	vGetDuration        = 19
	vIsPaused           = 20
	vIsEnded            = 27
	vPlay               = 32
	vPause              = 33
	vSetMuted           = 35
	vGetNativeVideoSize = 40
	vShutdown           = 42
	vTransferVideoFrame = 43
	vOnVideoStreamTick  = 44
	vSetCurrentTimeEx   = 80

	vCreateInstance = 3 // IMFMediaEngineClassFactory

	vSetUINT32  = 21 // IMFAttributes
	vSetUnknown = 27

	vResetDevice             = 7 // IMFDXGIDeviceManager
	vSetMultithreadProtected = 5 // ID3D10Multithread

	vCreateTexture2D = 5 // ID3D11Device

	vMap          = 14 // ID3D11DeviceContext
	vUnmap        = 15
	vCopyResource = 47

	vCreateBitmap = 17 // IWICImagingFactory
	vCopyPixels   = 7  // IWICBitmap
)

type texture2DDesc struct {
	width, height, mipLevels, arraySize, format uint32
	sampleCount, sampleQuality                  uint32
	usage, bindFlags, cpuAccessFlags, miscFlags uint32
}

type mappedSubresource struct {
	data                 unsafe.Pointer
	rowPitch, depthPitch uint32
}

// comObj is a COM interface pointer: a pointer to its vtable.
type comObj struct{ vtbl *[128]uintptr }

func (o *comObj) release() {
	if o != nil {
		syscall.SyscallN(o.vtbl[vRelease], uintptr(unsafe.Pointer(o)))
	}
}

func failed(hr uintptr) bool { return int32(hr) < 0 }

// notifyObj implements IMFMediaEngineNotify. It lives in Go memory, kept
// alive by the player, which shuts the engine down before dropping it.
type notifyObj struct {
	vtbl *[4]uintptr
	err  atomic.Bool
}

var (
	notifyVtblOnce sync.Once
	notifyVtbl     [4]uintptr
)

func newNotify() *notifyObj {
	notifyVtblOnce.Do(func() {
		notifyVtbl = [4]uintptr{
			syscall.NewCallback(func(this *notifyObj, riid *guid, ppv *unsafe.Pointer) uintptr {
				if *riid == iidIUnknown || *riid == iidIMFMediaEngineNotify {
					*ppv = unsafe.Pointer(this)
					return 0
				}
				*ppv = nil
				return eNoInterface
			}),
			syscall.NewCallback(func(this *notifyObj) uintptr { return 1 }), // AddRef: the player owns it
			syscall.NewCallback(func(this *notifyObj) uintptr { return 1 }), // Release
			syscall.NewCallback(func(this *notifyObj, event, param1, param2 uintptr) uintptr {
				if uint32(event) == eventError {
					this.err.Store(true)
				}
				return 0
			}),
		}
	})
	return &notifyObj{vtbl: &notifyVtbl}
}

// winPlayer is the state of the player's goroutine.
type winPlayer struct {
	p      *Player
	notify *notifyObj
	engine *comObj
	ex     *comObj // IMFMediaEngineEx, for fast seeks; may be nil
	source uintptr // BSTR
	mf     bool    // MFStartup succeeded

	// The GPU path.
	device, context, manager *comObj
	texture, staging         *comObj
	// The software path.
	wic, bitmap *comObj

	outSize image.Point // size of texture and staging, or of bitmap
}

func start(p *Player, path string) error {
	if mfplat.Load() != nil || ole32.Load() != nil || oleaut32.Load() != nil {
		return ErrUnsupported
	}
	errc := make(chan error, 1)
	go func() {
		// COM objects stay on the thread that made them.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		procCoInitializeEx.Call(0, coinitMultithreaded)
		defer procCoUninitialize.Call()
		w := &winPlayer{p: p}
		err := w.open(path)
		errc <- err
		if err != nil {
			w.close()
			return
		}
		w.run()
	}()
	return <-errc
}

func (w *winPlayer) open(path string) error {
	// Media Foundation counts startups; the last shutdown frees its threads.
	if hr, _, _ := procMFStartup.Call(mfVersion, 0); failed(hr) {
		return ErrUnsupported
	}
	w.mf = true
	var factory *comObj
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidMFMediaEngineClassFactory)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIMFMediaEngineClassFactory)), uintptr(unsafe.Pointer(&factory)))
	if failed(hr) {
		return ErrUnsupported // e.g. Windows N without the Media Feature Pack
	}
	defer factory.release()

	var attrs *comObj
	if hr, _, _ := procMFCreateAttributes.Call(uintptr(unsafe.Pointer(&attrs)), 3); failed(hr) {
		return errPlay
	}
	defer attrs.release()
	w.notify = newNotify()
	syscall.SyscallN(attrs.vtbl[vSetUnknown], uintptr(unsafe.Pointer(attrs)),
		uintptr(unsafe.Pointer(&mfMediaEngineCallback)), uintptr(unsafe.Pointer(w.notify)))
	syscall.SyscallN(attrs.vtbl[vSetUINT32], uintptr(unsafe.Pointer(attrs)),
		uintptr(unsafe.Pointer(&mfMediaEngineVideoOutputFormat)), dxgiFormatB8G8R8A8)
	switch {
	case w.p.audio:
		// Sound only: no Direct3D device or WIC bitmap to draw into.
	case w.initGPU():
		syscall.SyscallN(attrs.vtbl[vSetUnknown], uintptr(unsafe.Pointer(attrs)),
			uintptr(unsafe.Pointer(&mfMediaEngineDXGIManager)), uintptr(unsafe.Pointer(w.manager)))
	default:
		hr, _, _ = procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidWICImagingFactory)), 0, clsctxInprocServer,
			uintptr(unsafe.Pointer(&iidIWICImagingFactory)), uintptr(unsafe.Pointer(&w.wic)))
		if failed(hr) {
			return ErrUnsupported
		}
	}

	hr, _, _ = syscall.SyscallN(factory.vtbl[vCreateInstance], uintptr(unsafe.Pointer(factory)),
		0, uintptr(unsafe.Pointer(attrs)), uintptr(unsafe.Pointer(&w.engine)))
	if failed(hr) || w.engine == nil {
		return ErrUnsupported
	}
	syscall.SyscallN(w.engine.vtbl[vQueryInterface], uintptr(unsafe.Pointer(w.engine)),
		uintptr(unsafe.Pointer(&iidIMFMediaEngineEx)), uintptr(unsafe.Pointer(&w.ex)))

	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	u := (&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(abs)}).String()
	src, err := syscall.UTF16PtrFromString(u)
	if err != nil {
		return err
	}
	w.source, _, _ = procSysAllocString.Call(uintptr(unsafe.Pointer(src)))
	if hr, _, _ := syscall.SyscallN(w.engine.vtbl[vSetSource], uintptr(unsafe.Pointer(w.engine)), w.source); failed(hr) {
		return errPlay
	}
	syscall.SyscallN(w.engine.vtbl[vPlay], uintptr(unsafe.Pointer(w.engine)))
	return nil
}

// initGPU makes a Direct3D 11 device for the engine to decode on. It
// reports false when there is none; the engine then decodes in software.
func (w *winPlayer) initGPU() bool {
	if d3d11.Load() != nil || procMFCreateDXGIDevMgr.Find() != nil {
		return false
	}
	hr, _, _ := procD3D11CreateDevice.Call(0, d3dDriverTypeHardware, 0, d3dCreateBGRASupport|d3dCreateVideoSupport,
		0, 0, d3dSDKVersion, uintptr(unsafe.Pointer(&w.device)), 0, uintptr(unsafe.Pointer(&w.context)))
	if failed(hr) || w.device == nil {
		w.device, w.context = nil, nil
		return false
	}
	// The engine uses the device from its own threads.
	var mt *comObj
	syscall.SyscallN(w.device.vtbl[vQueryInterface], uintptr(unsafe.Pointer(w.device)),
		uintptr(unsafe.Pointer(&iidID3D10Multithread)), uintptr(unsafe.Pointer(&mt)))
	if mt != nil {
		syscall.SyscallN(mt.vtbl[vSetMultithreadProtected], uintptr(unsafe.Pointer(mt)), 1)
		mt.release()
	}
	var token uint32
	hr, _, _ = procMFCreateDXGIDevMgr.Call(uintptr(unsafe.Pointer(&token)), uintptr(unsafe.Pointer(&w.manager)))
	if !failed(hr) {
		hr, _, _ = syscall.SyscallN(w.manager.vtbl[vResetDevice], uintptr(unsafe.Pointer(w.manager)),
			uintptr(unsafe.Pointer(w.device)), uintptr(token))
	}
	if failed(hr) {
		w.releaseGPU()
		return false
	}
	return true
}

func (w *winPlayer) releaseGPU() {
	w.texture.release()
	w.staging.release()
	w.manager.release()
	w.context.release()
	w.device.release()
	w.texture, w.staging, w.manager, w.context, w.device = nil, nil, nil, nil, nil
}

func (w *winPlayer) close() {
	if w.engine != nil {
		syscall.SyscallN(w.engine.vtbl[vShutdown], uintptr(unsafe.Pointer(w.engine)))
	}
	w.ex.release()
	w.engine.release()
	w.releaseGPU()
	w.bitmap.release()
	w.wic.release()
	if w.source != 0 {
		procSysFreeString.Call(w.source)
	}
	if w.mf {
		procMFShutdown.Call()
	}
	runtime.KeepAlive(w.notify)
}

func (w *winPlayer) run() {
	defer w.close()
	t := time.NewTimer(0)
	defer t.Stop()
	for {
		select {
		case <-w.p.done:
			return
		case c := <-w.p.cmds:
			w.do(c)
		case <-t.C:
		}
		if w.poll() {
			t.Reset(pollPlaying)
		} else {
			t.Reset(pollIdle)
		}
	}
}

func (w *winPlayer) do(c command) {
	e := uintptr(unsafe.Pointer(w.engine))
	switch c.kind {
	case cmdPlay:
		syscall.SyscallN(w.engine.vtbl[vPlay], e)
	case cmdPause:
		syscall.SyscallN(w.engine.vtbl[vPause], e)
	case cmdSeek:
		secs := uintptr(math.Float64bits(c.pos.Seconds()))
		if w.ex != nil && !c.exact {
			syscall.SyscallN(w.ex.vtbl[vSetCurrentTimeEx], uintptr(unsafe.Pointer(w.ex)), secs, seekModeApproximate)
		} else {
			syscall.SyscallN(w.engine.vtbl[vSetCurrentTime], e, secs)
		}
	case cmdMute:
		on := uintptr(0)
		if c.on {
			on = 1
		}
		syscall.SyscallN(w.engine.vtbl[vSetMuted], e, on)
	}
}

// poll copies out a new frame, if there is one, and updates the status.
// It reports whether the video is playing. The engine returns doubles in
// XMM0, which syscall hands back as r2.
func (w *winPlayer) poll() bool {
	e := uintptr(unsafe.Pointer(w.engine))
	var st Status
	if w.notify.err.Load() {
		st.Err = errPlay
		w.p.setStatus(st)
		return false
	}
	rs, _, _ := syscall.SyscallN(w.engine.vtbl[vGetReadyState], e)
	st.Ready = uint16(rs) >= haveMetadata
	_, pos, _ := syscall.SyscallN(w.engine.vtbl[vGetCurrentTime], e)
	_, dur, _ := syscall.SyscallN(w.engine.vtbl[vGetDuration], e)
	paused, _, _ := syscall.SyscallN(w.engine.vtbl[vIsPaused], e)
	ended, _, _ := syscall.SyscallN(w.engine.vtbl[vIsEnded], e)
	st.Pos = seconds(math.Float64frombits(uint64(pos)))
	st.Dur = seconds(math.Float64frombits(uint64(dur)))
	st.Paused, st.Ended = paused != 0, ended != 0
	if st.Ready && !w.p.audio {
		var cx, cy uint32
		syscall.SyscallN(w.engine.vtbl[vGetNativeVideoSize], e, uintptr(unsafe.Pointer(&cx)), uintptr(unsafe.Pointer(&cy)))
		st.Size = image.Pt(int(cx), int(cy))
		var pts int64
		if hr, _, _ := syscall.SyscallN(w.engine.vtbl[vOnVideoStreamTick], e, uintptr(unsafe.Pointer(&pts))); hr == 0 {
			w.frame(st.Size)
		} else if failed(hr) && hr != sFalse {
			st.Err = errPlay
		}
	}
	w.p.setStatus(st)
	return !st.Paused && !st.Ended && st.Err == nil
}

// frame copies the current video frame into a free buffer, as RGBA.
func (w *winPlayer) frame(native image.Point) {
	sz := w.p.frameSize(native)
	if sz == (image.Point{}) {
		return
	}
	buf := w.p.buffer(sz)
	var ok bool
	if w.device != nil {
		ok = w.gpuFrame(sz, buf)
	} else {
		ok = w.wicFrame(sz, buf)
	}
	if !ok {
		return
	}
	px := buf.Pix // BGRA to RGBA
	for i := 0; i < len(px); i += 4 {
		px[i], px[i+2], px[i+3] = px[i+2], px[i], 0xff
	}
	w.p.publish(buf)
}

// transfer has the engine draw the current frame, scaled to sz, into dst.
func (w *winPlayer) transfer(dst *comObj, sz image.Point) bool {
	rect := [4]int32{0, 0, int32(sz.X), int32(sz.Y)} // RECT
	black := [4]byte{0, 0, 0, 0xff}                  // MFARGB
	hr, _, _ := syscall.SyscallN(w.engine.vtbl[vTransferVideoFrame], uintptr(unsafe.Pointer(w.engine)),
		uintptr(unsafe.Pointer(dst)), 0, uintptr(unsafe.Pointer(&rect)), uintptr(unsafe.Pointer(&black)))
	return !failed(hr)
}

// gpuFrame gets the frame through a render-target texture and a staging
// texture the CPU can read. buf receives BGRA.
func (w *winPlayer) gpuFrame(sz image.Point, buf *image.RGBA) bool {
	if w.texture == nil || w.outSize != sz {
		w.texture.release()
		w.staging.release()
		w.texture, w.staging = nil, nil
		desc := texture2DDesc{width: uint32(sz.X), height: uint32(sz.Y), mipLevels: 1, arraySize: 1,
			format: dxgiFormatB8G8R8A8, sampleCount: 1, bindFlags: d3dBindRenderTarget}
		dev := uintptr(unsafe.Pointer(w.device))
		if hr, _, _ := syscall.SyscallN(w.device.vtbl[vCreateTexture2D], dev, uintptr(unsafe.Pointer(&desc)), 0,
			uintptr(unsafe.Pointer(&w.texture))); failed(hr) {
			return false
		}
		desc.usage, desc.bindFlags, desc.cpuAccessFlags = d3dUsageStaging, 0, d3dCPUAccessRead
		if hr, _, _ := syscall.SyscallN(w.device.vtbl[vCreateTexture2D], dev, uintptr(unsafe.Pointer(&desc)), 0,
			uintptr(unsafe.Pointer(&w.staging))); failed(hr) {
			w.texture.release()
			w.texture = nil
			return false
		}
		w.outSize = sz
	}
	if !w.transfer(w.texture, sz) {
		return false
	}
	ctx := uintptr(unsafe.Pointer(w.context))
	syscall.SyscallN(w.context.vtbl[vCopyResource], ctx, uintptr(unsafe.Pointer(w.staging)), uintptr(unsafe.Pointer(w.texture)))
	var m mappedSubresource
	if hr, _, _ := syscall.SyscallN(w.context.vtbl[vMap], ctx, uintptr(unsafe.Pointer(w.staging)), 0, d3dMapRead, 0,
		uintptr(unsafe.Pointer(&m))); failed(hr) || m.data == nil {
		return false
	}
	pitch, row := int(m.rowPitch), 4*sz.X
	src := unsafe.Slice((*byte)(m.data), pitch*(sz.Y-1)+row)
	for y := 0; y < sz.Y; y++ {
		copy(buf.Pix[y*buf.Stride:y*buf.Stride+row], src[y*pitch:y*pitch+row])
	}
	syscall.SyscallN(w.context.vtbl[vUnmap], ctx, uintptr(unsafe.Pointer(w.staging)), 0)
	return true
}

// wicFrame gets the frame through a WIC bitmap. buf receives BGRA.
func (w *winPlayer) wicFrame(sz image.Point, buf *image.RGBA) bool {
	if w.bitmap == nil || w.outSize != sz {
		w.bitmap.release()
		w.bitmap = nil
		hr, _, _ := syscall.SyscallN(w.wic.vtbl[vCreateBitmap], uintptr(unsafe.Pointer(w.wic)), uintptr(sz.X), uintptr(sz.Y),
			uintptr(unsafe.Pointer(&guidWICPixelFormat32bppBGRA)), wicBitmapCacheOnLoad, uintptr(unsafe.Pointer(&w.bitmap)))
		if failed(hr) {
			return false
		}
		w.outSize = sz
	}
	if !w.transfer(w.bitmap, sz) {
		return false
	}
	hr, _, _ := syscall.SyscallN(w.bitmap.vtbl[vCopyPixels], uintptr(unsafe.Pointer(w.bitmap)), 0,
		uintptr(buf.Stride), uintptr(len(buf.Pix)), uintptr(unsafe.Pointer(&buf.Pix[0])))
	return !failed(hr)
}

// seconds converts, mapping unknown (NaN) and endless (+Inf) lengths to 0.
func seconds(s float64) time.Duration {
	if math.IsNaN(s) || math.IsInf(s, 0) || s < 0 {
		return 0
	}
	return time.Duration(s * float64(time.Second))
}
