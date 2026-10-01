// Package video plays video files with the operating system's own player,
// which decodes in hardware where it can and brings its own codecs, audio
// output and A/V sync. The app gets the picture frame by frame to draw.
//
// Each system has its own file (video_windows.go, ...). Where there is none,
// Open returns ErrUnsupported and the app hands videos to the system's
// video player app instead.
package video

import (
	"errors"
	"image"
	"sync"
	"time"
)

// ErrUnsupported means this system has no video backend.
var ErrUnsupported = errors.New("video: no video player on this system")

// Status is what a player is doing.
type Status struct {
	Ready  bool // the video's length and size are known
	Paused bool
	Ended  bool
	Pos    time.Duration
	Dur    time.Duration
	Size   image.Point // the picture's size, as shown (rotation applied)
	Err    error       // the video can't play
}

// Player plays one file. Its methods don't block: they queue commands for
// the player's own goroutine.
type Player struct {
	notify func()
	cmds   chan command
	done   chan struct{}
	once   sync.Once

	audio bool // no picture: OpenAudio

	mu    sync.Mutex
	st    Status
	want  image.Point // largest frame the app will draw
	bufs  [3]*image.RGBA
	ready int // newest frame, or -1
	shown int // frame the app took last, or -1
}

type cmdKind int

const (
	cmdPlay cmdKind = iota
	cmdPause
	cmdSeek
	cmdMute
)

type command struct {
	kind  cmdKind
	pos   time.Duration
	exact bool
	on    bool
}

// Open starts playing path. notify is called from other goroutines when
// there is a new frame or the status changed.
func Open(path string, notify func()) (*Player, error) {
	p := &Player{notify: notify, cmds: make(chan command, 16), done: make(chan struct{}), ready: -1, shown: -1}
	if err := start(p, path); err != nil {
		return nil, err
	}
	return p, nil
}

// OpenAudio is Open for a sound file. The player then sets nothing up to
// draw pictures with, and Frame always returns nil.
func OpenAudio(path string, notify func()) (*Player, error) {
	p := &Player{notify: notify, cmds: make(chan command, 16), done: make(chan struct{}), ready: -1, shown: -1, audio: true}
	if err := start(p, path); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Player) send(c command) {
	select {
	case p.cmds <- c:
	case <-p.done:
	}
}

func (p *Player) Play()  { p.send(command{kind: cmdPlay}) }
func (p *Player) Pause() { p.send(command{kind: cmdPause}) }

// Seek jumps to pos. An inexact seek lands on a nearby keyframe, which is
// fast enough to follow a dragged seek bar.
func (p *Player) Seek(pos time.Duration, exact bool) {
	p.send(command{kind: cmdSeek, pos: pos, exact: exact})
}

func (p *Player) SetMuted(on bool) { p.send(command{kind: cmdMute, on: on}) }

// Close stops playback and frees the player.
func (p *Player) Close() { p.once.Do(func() { close(p.done) }) }

// Status returns what the player is doing.
func (p *Player) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.st
}

// SetFrameSize sets the largest frame the app will draw, in pixels. Frames
// are scaled down to fit it, never up.
func (p *Player) SetFrameSize(max image.Point) {
	p.mu.Lock()
	p.want = max
	p.mu.Unlock()
}

// Frame returns the newest frame, and whether it changed since the last
// call. The app may draw it until the next call to Frame: the player never
// writes the frame it returned last.
func (p *Player) Frame() (img *image.RGBA, changed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ready >= 0 && p.ready != p.shown {
		p.shown, changed = p.ready, true
	}
	if p.shown < 0 {
		return nil, false
	}
	return p.bufs[p.shown], changed
}

// The rest is for the per-system backends.

// setStatus updates the status and tells the app if it changed.
func (p *Player) setStatus(st Status) {
	p.mu.Lock()
	changed := st != p.st
	p.st = st
	p.mu.Unlock()
	if changed && p.notify != nil {
		p.notify()
	}
}

// frameSize fits a native picture size into what the app asked for.
func (p *Player) frameSize(native image.Point) image.Point {
	p.mu.Lock()
	want := p.want
	p.mu.Unlock()
	if native.X <= 0 || native.Y <= 0 {
		return image.Point{}
	}
	if want.X <= 0 || want.Y <= 0 {
		want = image.Pt(1280, 1280)
	}
	s := min(1, float64(want.X)/float64(native.X), float64(want.Y)/float64(native.Y))
	return image.Pt(max(2, int(float64(native.X)*s)&^1), max(2, int(float64(native.Y)*s)&^1))
}

// buffer returns a frame buffer of size sz that is neither the newest frame
// nor the one the app shows.
func (p *Player) buffer(sz image.Point) *image.RGBA {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, b := range p.bufs {
		if i == p.ready || i == p.shown {
			continue
		}
		if b == nil || b.Rect.Size() != sz {
			b = image.NewRGBA(image.Rectangle{Max: sz})
			p.bufs[i] = b
		}
		return b
	}
	panic("unreachable")
}

// publish makes b, filled by the backend, the newest frame.
func (p *Player) publish(b *image.RGBA) {
	p.mu.Lock()
	for i := range p.bufs {
		if p.bufs[i] == b {
			p.ready = i
		}
	}
	p.mu.Unlock()
	if p.notify != nil {
		p.notify()
	}
}
