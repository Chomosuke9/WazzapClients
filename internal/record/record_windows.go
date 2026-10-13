//go:build windows

package record

// The Windows capture uses winmm's waveIn, which every Windows has and
// which converts whatever the default microphone records to the format
// asked for. It is called through syscall, without cgo.
//
// Four buffers of 100 ms go to the driver at a time. It fills them in
// order, sets WHDR_DONE and signals an event; the capture goroutine hands
// each full one to the sink and gives it back. The buffers and their
// headers are Go memory, pinned while the driver holds them.

import (
	"fmt"
	"os/exec"
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const supported = true

var (
	winmm = windows.NewLazySystemDLL("winmm.dll")

	procWaveInOpen            = winmm.NewProc("waveInOpen")
	procWaveInClose           = winmm.NewProc("waveInClose")
	procWaveInPrepareHeader   = winmm.NewProc("waveInPrepareHeader")
	procWaveInUnprepareHeader = winmm.NewProc("waveInUnprepareHeader")
	procWaveInAddBuffer       = winmm.NewProc("waveInAddBuffer")
	procWaveInStart           = winmm.NewProc("waveInStart")
	procWaveInReset           = winmm.NewProc("waveInReset")
)

// From mmeapi.h and mmsyscom.h.
const (
	waveMapper     = 0xffffffff
	callbackEvent  = 0x00050000
	whdrDone       = 0x00000001
	mmsysAllocated = 4
	mmsysBadDevice = 2
	mmsysNoDriver  = 6
	waveFormatPCM  = 1
	numBuffers     = 4
)

type waveFormatEx struct {
	formatTag      uint16
	channels       uint16
	samplesPerSec  uint32
	avgBytesPerSec uint32
	blockAlign     uint16
	bitsPerSample  uint16
	cbSize         uint16
}

// waveHdr is WAVEHDR. Its fields fall on their natural alignment, so
// mmsystem.h's byte packing lays it out the same.
type waveHdr struct {
	data          uintptr
	bufferLength  uint32
	bytesRecorded uint32
	user          uintptr
	flags         uint32
	loops         uint32
	next          uintptr
	reserved      uintptr
}

type mic struct {
	h     uintptr
	event windows.Handle
	hdrs  [numBuffers]*waveHdr
	bufs  [numBuffers][]byte
	pin   runtime.Pinner
	sink  func([]byte)
	fail  func(error)
	quit  chan struct{}
	done  chan struct{}
	ended atomic.Bool
}

func openMic(rate int, sink func([]byte), fail func(error)) (source, error) {
	ev, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return nil, err
	}
	m := &mic{event: ev, sink: sink, fail: fail, quit: make(chan struct{}), done: make(chan struct{})}
	f := &waveFormatEx{formatTag: waveFormatPCM, channels: 1, samplesPerSec: uint32(rate),
		avgBytesPerSec: uint32(rate * 2), blockAlign: 2, bitsPerSample: 16}
	r, _, _ := procWaveInOpen.Call(uintptr(unsafe.Pointer(&m.h)), waveMapper, uintptr(unsafe.Pointer(f)),
		uintptr(ev), 0, callbackEvent)
	if r != 0 {
		windows.CloseHandle(ev)
		return nil, mmError("open the microphone", r)
	}
	size := rate / 10 * 2 // 100 ms
	for i := range m.hdrs {
		m.bufs[i] = make([]byte, size)
		m.hdrs[i] = &waveHdr{data: uintptr(unsafe.Pointer(&m.bufs[i][0])), bufferLength: uint32(size)}
		m.pin.Pin(&m.bufs[i][0])
		m.pin.Pin(m.hdrs[i])
		if r, _, _ := procWaveInPrepareHeader.Call(m.h, uintptr(unsafe.Pointer(m.hdrs[i])), unsafe.Sizeof(waveHdr{})); r != 0 {
			m.close(i)
			return nil, mmError("prepare the microphone", r)
		}
	}
	for _, hdr := range m.hdrs {
		if r, _, _ := procWaveInAddBuffer.Call(m.h, uintptr(unsafe.Pointer(hdr)), unsafe.Sizeof(waveHdr{})); r != 0 {
			m.close(numBuffers)
			return nil, mmError("start the microphone", r)
		}
	}
	if r, _, _ := procWaveInStart.Call(m.h); r != 0 {
		m.close(numBuffers)
		return nil, mmError("start the microphone", r)
	}
	go m.run()
	return m, nil
}

// run hands the filled buffers to the sink, in order, until stop.
func (m *mic) run() {
	defer close(m.done)
	next := 0
	for {
		windows.WaitForSingleObject(m.event, 200)
		select {
		case <-m.quit:
			m.close(numBuffers)
			return
		default:
		}
		for range numBuffers {
			hdr := m.hdrs[next]
			if atomic.LoadUint32(&hdr.flags)&whdrDone == 0 {
				break
			}
			if n := atomic.LoadUint32(&hdr.bytesRecorded); n > 0 {
				m.sink(m.bufs[next][:n])
			}
			atomic.StoreUint32(&hdr.flags, atomic.LoadUint32(&hdr.flags)&^whdrDone)
			hdr.bytesRecorded = 0
			if r, _, _ := procWaveInAddBuffer.Call(m.h, uintptr(unsafe.Pointer(hdr)), unsafe.Sizeof(waveHdr{})); r != 0 {
				// The microphone went away (unplugged, or taken).
				if !m.ended.Swap(true) {
					m.fail(mmError("keep recording", r))
				}
			}
			next = (next + 1) % numBuffers
		}
	}
}

// close stops the device and frees it, with the first n headers prepared.
func (m *mic) close(n int) {
	procWaveInReset.Call(m.h) // returns every buffer, done
	for _, hdr := range m.hdrs[:n] {
		procWaveInUnprepareHeader.Call(m.h, uintptr(unsafe.Pointer(hdr)), unsafe.Sizeof(waveHdr{}))
	}
	procWaveInClose.Call(m.h)
	windows.CloseHandle(m.event)
	m.pin.Unpin()
}

func (m *mic) stop() {
	close(m.quit)
	windows.SetEvent(m.event)
	<-m.done
}

// mmError words a waveIn error for the app's message.
func mmError(doing string, r uintptr) error {
	switch r {
	case mmsysBadDevice, mmsysNoDriver:
		return ErrNoMic
	case mmsysAllocated:
		return fmt.Errorf("the microphone is in use by another app")
	}
	return fmt.Errorf("couldn't %s (error %d). Check that apps may use it in Settings > Privacy & security > Microphone", doing, r)
}

// hideWindow keeps ffmpeg from opening a console window.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
