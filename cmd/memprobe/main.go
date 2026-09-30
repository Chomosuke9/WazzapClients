//go:build windows

// Command memprobe measures the app's memory on Windows. It opens stored
// chats offline (no network) or demo data, visits every chat and page like
// a user clicking through, and prints the process memory along the way:
// the private working set is what Task Manager shows as "Memory".
//
//	go run ./cmd/memprobe -data "$APPDATA/WazzapClients"
//	go run ./cmd/memprobe -demo -heapprofile heap.pprof
//
// Point -data at a copy of the data directory while the app is running.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	rdebug "runtime/debug"
	"runtime/pprof"
	"slices"
	"syscall"
	"time"
	"unsafe"

	"gioui.org/app"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/memtrim"
	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/ui"
	"github.com/chomosuke9/wazzapclients/internal/wa"
)

// offline shows a stored session without connecting.
type offline struct {
	*wa.Backend
	started bool
}

func (o *offline) Start(func()) {}

func (o *offline) Poll() []model.Event {
	ev := o.Backend.Poll()
	if !o.started {
		o.started = true
		ev = append(ev, model.ConnEvent{State: model.StateOnline})
	}
	return ev
}

// PROCESS_MEMORY_COUNTERS_EX2
type memCounters struct {
	cb, pageFaults                  uint32
	peakWS, ws                      uintptr
	_, _, _, _                      uintptr
	pagefile, peakPagefile, private uintptr
	privateWS, sharedCommit         uint64
}

var getMemInfo = syscall.NewLazyDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")

func report(stage string) {
	var c memCounters
	c.cb = uint32(unsafe.Sizeof(c))
	h, _ := syscall.GetCurrentProcess()
	getMemInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&c)), uintptr(c.cb))
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	fmt.Printf("%-12s private WS %4d MB | WS %4d MB | commit %4d MB | Go heap in use %3d MB, from OS %3d MB\n",
		stage, c.privateWS>>20, c.ws>>20, c.private>>20, ms.HeapInuse>>20, (ms.HeapSys-ms.HeapReleased)>>20)
}

func main() {
	data := flag.String("data", "", "session data directory to show (use a copy)")
	demo := flag.Bool("demo", false, "use demo chats instead of -data")
	heapProfile := flag.String("heapprofile", "", "write a heap profile after visiting everything")
	passes := flag.Int("passes", 1, "times to visit every chat and page; later passes show what caches keep")
	flag.Parse()
	// As cmd/wazzap does.
	rdebug.SetGCPercent(50)
	rdebug.SetMemoryLimit(96 << 20)

	var backend model.Backend
	switch {
	case *demo:
		backend = mock.New()
	case *data != "":
		b, err := wa.Open(*data, false)
		if err != nil {
			log.Fatal(err)
		}
		backend = &offline{Backend: b}
	default:
		log.Fatal("need -data or -demo")
	}
	report("start")
	go func() {
		w := new(app.Window)
		w.Option(app.Title("memprobe"), app.Size(unit.Dp(1200), unit.Dp(780)), app.Decorated(false))
		u := ui.New(backend)
		u.Start(w.Invalidate)
		pages := []string{"status", "channels", "communities", "settings", "chats"}
		var ops op.Ops
		frame, step, trimmed, pass := 0, 0, 0, 1
		var times frameTimes
		for {
			switch e := w.Event().(type) {
			case app.DestroyEvent:
				os.Exit(0)
			case app.FrameEvent:
				t0 := time.Now()
				gtx := app.NewContext(&ops, e)
				u.Layout(gtx)
				t1 := time.Now()
				e.Frame(gtx.Ops)
				if frame >= 30 && trimmed == 0 {
					times.add(t1.Sub(t0), time.Since(t0))
				}
				frame++
				w.Invalidate()
				switch {
				case frame == 30:
					report("first paint")
				case trimmed > 0:
					// After a trim, keep drawing and open a chat, to see
					// what comes back.
					if frame == trimmed+60 {
						u.Select(0)
					}
					if frame == trimmed+240 {
						report("in use again")
						os.Exit(0)
					}
				case frame > 30 && frame%6 == 0:
					chats := len(backend.Chats())
					switch {
					case step < chats:
						u.Select(step)
					case step-chats < len(pages):
						u.ShowPage(pages[step-chats])
					case pass < *passes:
						times.report(pass)
						pass, step = pass+1, -1
					default:
						times.report(pass)
						report("visited all")
						runtime.GC()
						if *heapProfile != "" {
							writeHeapProfile(*heapProfile)
						}
						rdebug.FreeOSMemory()
						report("heap freed")
						memtrim.Trim()
						report("trimmed")
						trimmed = frame
					}
					step++
				}
			}
		}
	}()
	app.Main()
}

func writeHeapProfile(path string) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := pprof.Lookup("heap").WriteTo(f, 0); err != nil {
		log.Fatal(err)
	}
}

// frameTimes collects how long frames take on the CPU during a pass.
type frameTimes struct {
	layout, frame []time.Duration
	cpu0          time.Duration
	gc0           uint32
}

func (t *frameTimes) add(layout, frame time.Duration) {
	if t.layout == nil {
		t.cpu0, t.gc0 = cpuTime(), gcCount()
	}
	t.layout = append(t.layout, layout)
	t.frame = append(t.frame, frame)
}

// report prints the pass's frame times: layout is building the frame (text
// shaping included), frame adds rendering it (which may wait for vsync).
func (t *frameTimes) report(pass int) {
	stat := func(d []time.Duration) string {
		s := slices.Clone(d)
		slices.Sort(s)
		var sum time.Duration
		for _, v := range s {
			sum += v
		}
		ms := func(v time.Duration) float64 { return float64(v) / 1e6 }
		return fmt.Sprintf("avg %5.2f p95 %6.2f max %6.2f ms", ms(sum/time.Duration(len(s))), ms(s[len(s)*95/100]), ms(s[len(s)-1]))
	}
	fmt.Printf("pass %d: %d frames | layout %s | frame %s | process CPU %.2f s, %d GCs\n", pass, len(t.layout),
		stat(t.layout), stat(t.frame), (cpuTime() - t.cpu0).Seconds(), gcCount()-t.gc0)
	*t = frameTimes{}
}

var getProcessTimes = syscall.NewLazyDLL("kernel32.dll").NewProc("GetProcessTimes")

// cpuTime returns the CPU time the process has used, user and kernel.
func cpuTime() time.Duration {
	var created, exited, kernel, user syscall.Filetime
	h, _ := syscall.GetCurrentProcess()
	getProcessTimes.Call(uintptr(h), uintptr(unsafe.Pointer(&created)), uintptr(unsafe.Pointer(&exited)),
		uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	ticks := func(f syscall.Filetime) int64 { return int64(f.HighDateTime)<<32 | int64(f.LowDateTime) }
	return time.Duration(ticks(kernel)+ticks(user)) * 100
}

func gcCount() uint32 {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.NumGC
}
