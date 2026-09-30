package ui

import (
	"image"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/op/paint"
	xdraw "golang.org/x/image/draw"

	"github.com/chomosuke9/wazzapclients/internal/webpanim"
)

// maxPlayers caps how many animated stickers play at once, and
// playerBudget the memory they may hold together: a normal 512x512 sticker
// takes about 2.5 MB, but a sticker's canvas can be up to 4096x4096. The
// rest show their first frame.
const (
	maxPlayers   = 6
	playerBudget = 24 << 20
)

// stickerPlayer plays an animated sticker. A goroutine renders each frame
// ahead of time into one of three buffers, so it never writes the one the
// UI is showing (Gio uploads it while the frame renders) or the newest.
type stickerPlayer struct {
	mu    sync.Mutex
	bufs  [3]*image.RGBA
	ready int // newest rendered buffer, or -1
	shown int // buffer the UI showed last, or -1
	op    paint.ImageOp
	used  int64 // frame counter of last use
	stop  chan struct{}
}

// players holds the animated stickers on screen, by image cache key.
type players struct {
	m     map[string]*stickerPlayer
	frame int64
	bytes atomic.Int64 // held by the running players, against playerBudget
}

// stickerFrame returns the current frame of an animated sticker, starting
// its player. ok is false while the first frame renders, or when too many
// stickers already play.
func (u *UI) stickerFrame(key string, maxSide int, load func() []byte) (paint.ImageOp, bool) {
	ps := &u.players
	p := ps.m[key]
	if p == nil {
		if len(ps.m) >= maxPlayers {
			return paint.ImageOp{}, false
		}
		if ps.m == nil {
			ps.m = make(map[string]*stickerPlayer)
		}
		p = &stickerPlayer{ready: -1, shown: -1, stop: make(chan struct{})}
		ps.m[key] = p
		go p.run(load, maxSide, &ps.bytes, u.images.invalidate)
	}
	p.used = ps.frame
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ready >= 0 && p.ready != p.shown {
		// The frame that showed last has been drawn by now: switch.
		p.shown = p.ready
		p.op = paint.NewImageOp(p.bufs[p.shown])
	}
	return p.op, p.shown >= 0
}

// endFrame stops the players of stickers that weren't drawn this frame.
func (ps *players) endFrame() {
	for k, p := range ps.m {
		if p.used < ps.frame {
			close(p.stop)
			delete(ps.m, k)
		}
	}
	ps.frame++
}

func (p *stickerPlayer) run(load func() []byte, maxSide int, held *atomic.Int64, invalidate func()) {
	a, err := webpanim.Parse(load())
	if err != nil {
		return // the still first frame stays
	}
	w, h := a.Width, a.Height
	if s := max(w, h); s > maxSide {
		w, h = max(1, w*maxSide/s), max(1, h*maxSide/s)
	}
	// The canvas, a decoded frame (up to the canvas's size) and the three
	// buffers.
	cost := int64(a.Width)*int64(a.Height)*8 + 3*int64(w)*int64(h)*4
	if held.Add(cost) > playerBudget {
		held.Add(-cost)
		return
	}
	defer held.Add(-cost)
	due := time.Now()
	for n := 0; ; n++ {
		canvas, delay, err := a.Next()
		if err != nil {
			return
		}
		buf := p.free(w, h)
		if w == a.Width && h == a.Height {
			copy(buf.Pix, canvas.Pix)
		} else {
			xdraw.ApproxBiLinear.Scale(buf, buf.Bounds(), canvas, canvas.Bounds(), xdraw.Src, nil)
		}
		select {
		case <-p.stop:
			return
		case <-time.After(time.Until(due)):
		}
		p.mu.Lock()
		p.ready = p.index(buf)
		p.mu.Unlock()
		if invalidate != nil {
			invalidate()
		}
		if a.Loops > 0 && n+1 >= a.Loops*len(a.Frames) {
			return // played as often as it asks; the last frame stays
		}
		due = due.Add(delay)
		if now := time.Now(); due.Before(now.Add(-time.Second)) {
			due = now // fell far behind (e.g. the machine slept)
		}
	}
}

// free returns a buffer that is neither shown nor the newest.
func (p *stickerPlayer) free(w, h int) *image.RGBA {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, b := range p.bufs {
		if i == p.ready || i == p.shown {
			continue
		}
		if b == nil {
			b = image.NewRGBA(image.Rect(0, 0, w, h))
			p.bufs[i] = b
		}
		return b
	}
	panic("unreachable")
}

func (p *stickerPlayer) index(b *image.RGBA) int {
	for i := range p.bufs {
		if p.bufs[i] == b {
			return i
		}
	}
	return -1
}
