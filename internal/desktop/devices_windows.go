//go:build windows

package desktop

import (
	"syscall"
	"unsafe"
)

var (
	winmm                 = syscall.NewLazyDLL("winmm.dll")
	procWaveInGetNumDevs  = winmm.NewProc("waveInGetNumDevs")
	procWaveInGetDevCaps  = winmm.NewProc("waveInGetDevCapsW")
	procWaveOutGetNumDevs = winmm.NewProc("waveOutGetNumDevs")
	procWaveOutGetDevCaps = winmm.NewProc("waveOutGetDevCapsW")
)

// waveCaps is WAVEOUTCAPSW, which starts like WAVEINCAPSW (one DWORD
// shorter): only the name is read.
type waveCaps struct {
	mid, pid uint16
	version  uint32
	name     [32]uint16
	formats  uint32
	channels uint16
	reserved uint16
	support  uint32
}

// Microphones lists the names of the system's audio inputs. Windows cuts
// each name to 31 characters.
func Microphones() []string {
	return waveDevices(procWaveInGetNumDevs, procWaveInGetDevCaps, unsafe.Offsetof(waveCaps{}.support))
}

// Speakers lists the names of the system's audio outputs.
func Speakers() []string {
	return waveDevices(procWaveOutGetNumDevs, procWaveOutGetDevCaps, unsafe.Sizeof(waveCaps{}))
}

// waveDevices lists the names of winmm's devices; size is their caps
// struct's.
func waveDevices(count, caps *syscall.LazyProc, size uintptr) []string {
	if winmm.Load() != nil {
		return nil
	}
	n, _, _ := count.Call()
	var names []string
	for i := range n {
		var c waveCaps
		r, _, _ := caps.Call(i, uintptr(unsafe.Pointer(&c)), size)
		if r != 0 {
			continue
		}
		if s := syscall.UTF16ToString(c.name[:]); s != "" {
			names = append(names, s)
		}
	}
	return names
}
