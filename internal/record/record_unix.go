//go:build linux || freebsd || openbsd || netbsd

package record

// Elsewhere than Windows the capture runs PulseAudio's (or PipeWire's)
// parec, or ALSA's arecord, which write raw samples to a pipe.

import (
	"errors"
	"io"
	"os/exec"
	"strconv"
	"sync/atomic"
)

const supported = true

type mic struct {
	cmd      *exec.Cmd
	stopping atomic.Bool
	done     chan struct{}
}

func openMic(rate int, sink func([]byte), fail func(error)) (source, error) {
	r := strconv.Itoa(rate)
	var cmd *exec.Cmd
	switch {
	case has("parec"):
		cmd = exec.Command("parec", "--raw", "--format=s16le", "--rate="+r, "--channels=1", "--latency-msec=100")
	case has("arecord"):
		cmd = exec.Command("arecord", "-q", "-t", "raw", "-f", "S16_LE", "-r", r, "-c", "1")
	default:
		return nil, errors.New("recording needs parec (PulseAudio or PipeWire) or arecord (ALSA)")
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	m := &mic{cmd: cmd, done: make(chan struct{})}
	go func() {
		defer close(m.done)
		buf := make([]byte, rate/10*2) // 100 ms
		for {
			n, err := io.ReadFull(out, buf)
			if n > 0 {
				sink(buf[:n&^1])
			}
			if err != nil {
				if !m.stopping.Load() {
					fail(errors.New("the recorder stopped"))
				}
				return
			}
		}
	}()
	return m, nil
}

func has(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func (m *mic) stop() {
	m.stopping.Store(true)
	m.cmd.Process.Kill()
	<-m.done
	m.cmd.Wait()
}

func hideWindow(*exec.Cmd) {}
