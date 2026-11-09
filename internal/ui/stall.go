package ui

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"
)

// The window answers the compositor's pings (Wayland's xdg_wm_base) only
// while Gio reads its events, and the goroutine that reads them waits for
// the UI goroutine to handle each one (openWindow). So whatever holds the
// UI goroutine for a few seconds makes the compositor think the app hung:
// Hyprland shows "Application Not Responding" after misc:anr_missed_pings.
//
// stallWatch writes what the UI goroutine was doing, and every goroutine's
// stack, to Options.StallLog when one job holds it longer than stallAfter.
const stallAfter = 2 * time.Second

type stallWatch struct {
	path string

	mu       sync.Mutex
	job      string    // what the UI goroutine is doing, or ""
	since    time.Time // when it started
	reported bool      // the stacks of this job were written
	wake     chan struct{}
}

func newStallWatch(path string) *stallWatch {
	s := &stallWatch{path: path, wake: make(chan struct{}, 1)}
	if path != "" {
		go s.watch()
	}
	return s
}

// begin says the UI goroutine starts job; end, that it's done.
func (s *stallWatch) begin(job string) {
	if s.path == "" {
		return
	}
	s.mu.Lock()
	s.job, s.since, s.reported = job, time.Now(), false
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *stallWatch) end() {
	if s.path == "" {
		return
	}
	s.mu.Lock()
	job, took, reported := s.job, time.Since(s.since), s.reported
	s.job = ""
	s.mu.Unlock()
	if reported {
		s.write(fmt.Sprintf("%s finished after %s\n\n", job, took.Round(time.Millisecond)))
	}
}

func (s *stallWatch) watch() {
	for {
		<-s.wake
		for {
			time.Sleep(stallAfter / 4)
			s.mu.Lock()
			job, since, reported := s.job, s.since, s.reported
			late := job != "" && !reported && time.Since(since) >= stallAfter
			if late {
				s.reported = true
			}
			s.mu.Unlock()
			if job == "" {
				break
			}
			if late {
				buf := make([]byte, 1<<20)
				buf = buf[:runtime.Stack(buf, true)]
				s.write(fmt.Sprintf("%s: the UI goroutine has been on %s for %s\n%s\n",
					time.Now().Format("2006/01/02 15:04:05.000"), job,
					time.Since(since).Round(time.Millisecond), buf))
			}
		}
	}
}

func (s *stallWatch) write(text string) {
	// Keep the file small: a stall's stacks take tens of kilobytes.
	if st, err := os.Stat(s.path); err == nil && st.Size() > 2<<20 {
		_ = os.Truncate(s.path, 0)
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	_, _ = f.WriteString(text)
	_ = f.Close()
}
