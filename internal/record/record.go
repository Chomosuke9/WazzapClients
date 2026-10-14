// Package record records voice messages from the microphone.
//
// The microphone's sound streams, as 16 kHz mono 16-bit PCM, into a WAV
// file in the temporary folder, so a long message costs disk, not memory;
// only its loudness is kept, one level every 50 ms. Pausing closes the file,
// which the system's player can then play back (the preview). Finish
// encodes it to Opus in OGG, as WhatsApp sends voice messages, through the
// system's ffmpeg (found on PATH, never bundled).
//
// Each system has its own capture (record_windows.go: winmm's waveIn
// through syscall, without cgo; record_unix.go: parec or arecord). Where
// there is none, Start returns ErrUnsupported.
package record

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Rate is the sample rate recorded at, WhatsApp's own for voice messages.
const Rate = 16000

// levelSamples is how many samples one loudness level covers (50 ms).
const levelSamples = Rate / 20

// WaveformLen is how many loudness samples a voice message carries, as
// WhatsApp sends them.
const WaveformLen = 64

var (
	// ErrUnsupported means this system has no microphone capture here.
	ErrUnsupported = errors.New("recording voice messages isn't supported on this system")
	// ErrNoFFmpeg means ffmpeg, which encodes the message, isn't on PATH.
	ErrNoFFmpeg = errors.New("voice messages need ffmpeg, which wasn't found. Install it and add it to PATH")
	// ErrNoMic means the system has no microphone.
	ErrNoMic = errors.New("no microphone was found")
)

// Demo makes Start record a made-up voice instead of the microphone (the
// app's -demo).
var Demo bool

// source is a running capture. It hands chunks of little-endian 16-bit
// samples to its sink from a goroutine of its own, and stop waits until
// it hands no more.
type source interface {
	stop()
}

// Check reports why recording can't work here, or nil.
func Check() error {
	if Demo {
		return nil
	}
	if !supported {
		return ErrUnsupported
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return ErrNoFFmpeg
	}
	return nil
}

// Session is a voice message being recorded. Its methods are safe to call
// from any goroutine, but Finish and Discard end it.
type Session struct {
	notify func()

	mu     sync.Mutex
	path   string   // the WAV file
	f      *os.File // open while recording
	n      int64    // samples kept
	levels []byte   // loudness, 0 to 100, one per levelSamples
	acc    float64  // sum of squares of the level being measured
	accN   int
	src    source
	paused bool
	err    error // the capture failed
}

// Start starts recording. notify is called, from other goroutines, when
// the capture fails.
func Start(notify func()) (*Session, error) {
	if err := Check(); err != nil {
		return nil, err
	}
	dir := filepath.Join(os.TempDir(), "WazzapClients")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(dir, "voice-*.wav")
	if err != nil {
		return nil, err
	}
	s := &Session{notify: notify, path: f.Name(), f: f, paused: true}
	if _, err := f.Write(wavHeader(0)); err != nil {
		f.Close()
		os.Remove(s.path)
		return nil, err
	}
	if err := s.Resume(); err != nil {
		s.Discard()
		return nil, err
	}
	return s, nil
}

// Sample returns a session that shows elapsed of made-up recording,
// paused or not, for screenshots. It records nothing.
func Sample(elapsed time.Duration, paused bool) *Session {
	s := &Session{paused: paused, n: int64(elapsed.Seconds() * Rate)}
	r := rand.New(rand.NewPCG(7, 7))
	for i := range int(s.n / levelSamples) {
		s.levels = append(s.levels, byte(100*demoLevel(float64(i)/20, r)))
	}
	return s
}

// Resume goes on recording after Pause.
func (s *Session) Resume() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.paused || s.path == "" {
		return nil
	}
	if s.f == nil {
		f, err := os.OpenFile(s.path, os.O_RDWR, 0)
		if err != nil {
			return err
		}
		if _, err := f.Seek(0, 2); err != nil {
			f.Close()
			return err
		}
		s.f = f
	}
	var (
		src source
		err error
	)
	if Demo {
		src = startDemo(s.write)
	} else {
		src, err = openMic(Rate, s.write, s.fail)
	}
	if err != nil {
		return err
	}
	s.src, s.paused, s.err = src, false, nil
	return nil
}

// Pause stops recording for now and closes the file, which Path then
// names, complete, for playing it back.
func (s *Session) Pause() error {
	s.mu.Lock()
	src := s.src
	s.src, s.paused = nil, true
	s.mu.Unlock()
	if src != nil {
		src.stop() // outside the lock: the sink takes it
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeFile()
}

// closeFile writes the header's sizes and closes the file.
func (s *Session) closeFile() error {
	if s.f == nil {
		return nil
	}
	_, err := s.f.WriteAt(wavHeader(s.n*2), 0)
	if cerr := s.f.Close(); err == nil {
		err = cerr
	}
	s.f = nil
	return err
}

// Path is the recording's WAV file, complete while paused.
func (s *Session) Path() string { return s.path }

// Paused reports whether recording is paused.
func (s *Session) Paused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paused
}

// Err is why the capture stopped, or nil.
func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Elapsed is how long the recording is.
func (s *Session) Elapsed() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Duration(s.n) * time.Second / Rate
}

// Levels fills dst with the latest loudness levels, from 0 to 1, the
// newest last. Before there are enough, dst starts with zeros. It returns
// how many are real.
func (s *Session) Levels(dst []float32) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := min(len(dst), len(s.levels))
	clear(dst[:len(dst)-k])
	for i, l := range s.levels[len(s.levels)-k:] {
		dst[len(dst)-k+i] = float32(l) / 100
	}
	return k
}

// Waveform is the recording's loudness as WhatsApp sends it: WaveformLen
// values from 0 to 100, the loudest 100.
func (s *Session) Waveform() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return waveform(s.levels)
}

func waveform(levels []byte) []byte {
	out := make([]byte, WaveformLen)
	if len(levels) == 0 {
		return out
	}
	var top byte
	for i := range out {
		a, b := i*len(levels)/WaveformLen, (i+1)*len(levels)/WaveformLen
		b = max(b, a+1)
		var v byte
		for _, l := range levels[a:min(b, len(levels))] {
			v = max(v, l)
		}
		out[i] = v
		top = max(top, v)
	}
	if top > 0 {
		for i, v := range out {
			out[i] = byte(int(v) * 100 / int(top))
		}
	}
	return out
}

// Discard stops recording and deletes it.
func (s *Session) Discard() {
	s.Pause()
	if s.path != "" {
		os.Remove(s.path)
	}
}

// Finish stops recording and encodes it into an OGG Opus file in the
// temporary folder, which the caller deletes once it is sent. It returns
// the file, the length in whole seconds (at least 1) and the waveform. It
// takes a moment (ffmpeg): call it off the UI goroutine.
func (s *Session) Finish() (path string, secs int, wave []byte, err error) {
	if err := s.Pause(); err != nil {
		s.Discard()
		return "", 0, nil, err
	}
	secs = max(1, int(math.Round(s.Elapsed().Seconds())))
	wave = s.Waveform()
	if s.path == "" {
		return "", secs, wave, errors.New("nothing was recorded")
	}
	out := strings.TrimSuffix(s.path, ".wav") + ".ogg"
	if err := encode(s.path, out); err != nil {
		if !Demo {
			s.Discard()
			return "", 0, nil, err
		}
		// The demo sends in name only: the WAV will do.
		return s.path, secs, wave, nil
	}
	os.Remove(s.path)
	return out, secs, wave, nil
}

// encode turns the WAV file in into Opus in OGG, mono, at WhatsApp's
// voice bitrate. ffmpeg's own Opus encoder stands in when it was built
// without libopus.
func encode(in, out string) error {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		return ErrNoFFmpeg
	}
	var msg string
	for _, codec := range [][]string{{"-c:a", "libopus", "-application", "voip"}, {"-c:a", "opus", "-strict", "-2"}} {
		args := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", in, "-vn", "-ac", "1", "-ar", "48000"}
		args = append(args, codec...)
		args = append(args, "-b:a", "32k", "-f", "ogg", out)
		cmd := exec.Command(ff, args...)
		hideWindow(cmd)
		b, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		msg = strings.TrimSpace(string(b))
		if msg == "" {
			msg = err.Error()
		}
	}
	os.Remove(out)
	return fmt.Errorf("ffmpeg couldn't encode it: %s", msg)
}

// write is the capture's sink: it appends samples to the file and
// measures their loudness.
func (s *Session) write(pcm []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.paused || s.f == nil {
		return
	}
	pcm = pcm[:len(pcm)&^1]
	if _, err := s.f.Write(pcm); err != nil {
		s.err = err
		if s.notify != nil {
			go s.notify()
		}
		return
	}
	s.n += int64(len(pcm) / 2)
	for i := 0; i < len(pcm); i += 2 {
		v := float64(int16(binary.LittleEndian.Uint16(pcm[i:])))
		s.acc += v * v
		if s.accN++; s.accN == levelSamples {
			s.levels = append(s.levels, level(s.acc/levelSamples))
			s.acc, s.accN = 0, 0
		}
	}
}

// fail is the capture's report that it stopped.
func (s *Session) fail(err error) {
	s.mu.Lock()
	s.err = err
	s.mu.Unlock()
	if s.notify != nil {
		s.notify()
	}
}

// level maps a mean square to a loudness from 0 to 100: -55 dBFS (quiet
// room) to 0 dBFS, which follows what the ear hears better than the
// amplitude does.
func level(ms float64) byte {
	if ms <= 0 {
		return 0
	}
	db := 10 * math.Log10(ms/(32768*32768))
	return byte(100 * min(1, max(0, (db+55)/55)))
}

// wavHeader is a 44-byte WAV header for size bytes of the PCM recorded.
func wavHeader(size int64) []byte {
	size = min(size, math.MaxUint32-36)
	h := make([]byte, 44)
	le := binary.LittleEndian
	copy(h, "RIFF")
	le.PutUint32(h[4:], uint32(36+size))
	copy(h[8:], "WAVEfmt ")
	le.PutUint32(h[16:], 16)
	le.PutUint16(h[20:], 1) // PCM
	le.PutUint16(h[22:], 1) // mono
	le.PutUint32(h[24:], Rate)
	le.PutUint32(h[28:], Rate*2)
	le.PutUint16(h[32:], 2)
	le.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	le.PutUint32(h[40:], uint32(size))
	return h
}

// demoLevel is a made-up voice's loudness at t seconds: syllables in
// words, with pauses between them.
func demoLevel(t float64, r *rand.Rand) float64 {
	word := 0.5 + 0.5*math.Sin(t*2*math.Pi*0.45)
	syl := 0.55 + 0.45*math.Abs(math.Sin(t*2*math.Pi*2.3))
	return min(1, max(0.04, 0.85*word*syl*(0.8+0.2*r.Float64())))
}

// demo is the made-up voice Demo records: noise shaped like demoLevel,
// in real time.
type demo struct {
	quit, done chan struct{}
}

func startDemo(sink func([]byte)) source {
	d := &demo{quit: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(d.done)
		r := rand.New(rand.NewPCG(1, 2))
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		buf := make([]byte, levelSamples*2)
		var t float64
		for {
			select {
			case <-d.quit:
				return
			case <-tick.C:
			}
			a := demoLevel(t, r)
			// Loudness back to an amplitude: level's mapping, reversed.
			amp := 32768 * math.Pow(10, (a*55-55)/20) * math.Sqrt2
			for i := range levelSamples {
				v := amp * math.Sin(float64(i)*0.3) * (0.7 + 0.3*r.Float64())
				binary.LittleEndian.PutUint16(buf[2*i:], uint16(int16(max(-32767, min(32767, v)))))
			}
			sink(buf)
			t += 0.05
		}
	}()
	return d
}

func (d *demo) stop() {
	close(d.quit)
	<-d.done
}
