package ui

import (
	"image"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/op/paint"
	xdraw "golang.org/x/image/draw"

	"github.com/chomosuke9/wazzapclients/internal/lottie"
	"github.com/chomosuke9/wazzapclients/internal/webpanim"
)

// perf.stickers (6 by default) animated stickers play at once, and
// playerBudget, stickerBudget for each, caps the memory they hold
// together: a normal 512x512 WebP sticker takes about 2.5 MB (a canvas can
// be up to 4096x4096), a Lottie one drawn 225 px wide about 1 MB. The rest
// show their first frame. A sticker clicked to play stops others until it
// fits (makeRoom). Players read the budget on their own goroutines.
var playerBudget = func() *atomic.Int64 {
	var v atomic.Int64
	v.Store(int64(defaultPerf.stickers) * stickerBudget)
	return &v
}()

const (
	stickerBudget = 4 << 20
	// roomWait is how long a clicked sticker waits for the players it
	// stopped to let go of their memory.
	roomWait = 2 * time.Second
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
	// preview marks a GIF or sticker playing in the picker (gifs.go,
	// stickers.go), which counts against a cap of its own.
	preview bool
	// fx marks a premium sticker's effect (stickerfx.go), which plays once
	// and counts against no cap; done is set once it has played (or
	// can't), and none when the sticker has no effect.
	fx         bool
	done, none atomic.Bool
	// seq orders the players by when they started, and clicked marks one
	// started by a click, which waits for room in the budget (makeRoom)
	// instead of giving up. cost is what it holds against the budget.
	seq     int64
	clicked bool
	cost    atomic.Int64
	// lottie is set once the sticker is found to be a Lottie one, which
	// its message may not say.
	lottie atomic.Bool
}

// players holds the animated stickers on screen, by image cache key.
type players struct {
	m        map[string]*stickerPlayer
	previews int // of m, the picker's GIFs
	effects  int // of m, premium stickers' effects
	// forced holds the stickers clicked to play, which may pass the cap
	// (and play with autoplay off) until they leave the screen.
	forced map[string]bool
	// benched holds the stickers stopped to make room for a clicked one
	// (with the frame they were last drawn): they don't start again until
	// they leave the screen or are clicked.
	benched map[string]int64
	frame   int64
	seq     int64
	bytes   atomic.Int64 // held by the running players, against playerBudget

	gifs     map[string]*gifPlayer // the GIFs playing, by "chat/id"
	gifFetch map[string]gifFetch

	// sound is the video note playing with sound, by "chat/id" (roundvideo.go).
	sound     string
	soundOn   bool  // its player has been told
	soundUsed int64 // frame of last use
}

// stickerFrame returns the current frame of an animated sticker, starting
// its player. ok is false while the first frame renders, or when too many
// stickers already play.
func (u *UI) stickerFrame(key string, maxSide int, load func() []byte) (paint.ImageOp, bool) {
	return u.animFrame(key, maxSide, false, load)
}

// previewFrame is stickerFrame for the picker: a GIF, an animated WebP a
// few hundred px across, or a sticker. Four times maxGIFs of them play at
// once.
func (u *UI) previewFrame(key string, maxSide int, load func() []byte) (paint.ImageOp, bool) {
	return u.animFrame(key, maxSide, true, load)
}

func (u *UI) animFrame(key string, maxSide int, preview bool, load func() []byte) (paint.ImageOp, bool) {
	ps := &u.players
	p := ps.m[key]
	if p == nil {
		n, limit := len(ps.m)-ps.previews-ps.effects, perf.stickers
		if preview {
			n, limit = ps.previews, 4*maxGIFs
		}
		if _, ok := ps.benched[key]; ok && !ps.forced[key] {
			ps.benched[key] = ps.frame
			return paint.ImageOp{}, false
		}
		if n >= limit && limit > 0 && !ps.forced[key] {
			return paint.ImageOp{}, false
		}
		p = ps.add(key, &stickerPlayer{preview: preview, clicked: ps.forced[key]})
		if preview {
			ps.previews++
		}
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

// endFrame stops the players of stickers and GIFs that weren't drawn this
// frame.
func (ps *players) endFrame() {
	for k, p := range ps.m {
		if p.used < ps.frame {
			ps.stop(k, p)
			delete(ps.forced, k)
		}
	}
	for k, f := range ps.benched {
		if f < ps.frame {
			delete(ps.benched, k)
		}
	}
	for k, g := range ps.gifs {
		if g.used < ps.frame {
			g.close()
			delete(ps.gifs, k)
		}
	}
	if ps.sound != "" && ps.soundUsed < ps.frame {
		ps.sound, ps.soundOn = "", false // scrolled away
	}
	ps.frame++
}

// stopAll stops every player.
func (ps *players) stopAll() {
	for k, p := range ps.m {
		ps.stop(k, p)
	}
	for k, g := range ps.gifs {
		g.close()
		delete(ps.gifs, k)
	}
	ps.sound, ps.soundOn = "", false
}

// add starts keeping p, a new player, as key's.
func (ps *players) add(key string, p *stickerPlayer) *stickerPlayer {
	if ps.m == nil {
		ps.m = make(map[string]*stickerPlayer)
	}
	ps.seq++
	p.ready, p.shown, p.stop, p.seq = -1, -1, make(chan struct{}), ps.seq
	ps.m[key] = p
	return p
}

// makeRoom stops players, other than those of keep, until need more bytes
// fit in the budget: those playing on their own first, then those clicked
// before, the oldest first. The ones stopped are benched.
func (ps *players) makeRoom(need int64, keep ...string) {
	held := ps.bytes.Load()
	for held+need > playerBudget.Load() {
		var victim string
		var vp *stickerPlayer
		for k, p := range ps.m {
			if p.preview || slices.Contains(keep, k) || p.cost.Load() == 0 {
				continue
			}
			if vp == nil || (!p.clicked && vp.clicked) || (p.clicked == vp.clicked && p.seq < vp.seq) {
				victim, vp = k, p
			}
		}
		if vp == nil {
			return
		}
		held -= vp.cost.Load()
		ps.stop(victim, vp)
		delete(ps.forced, victim)
		if ps.benched == nil {
			ps.benched = make(map[string]int64)
		}
		ps.benched[victim] = ps.frame
	}
}

// reserve takes cost bytes of the budget for p, or reports false when they
// don't fit. A clicked player waits for the players stopped to make room
// for it to let go of theirs.
func (p *stickerPlayer) reserve(held *atomic.Int64, cost int64) bool {
	deadline := time.Now().Add(roomWait)
	for {
		if held.Add(cost) <= playerBudget.Load() {
			p.cost.Add(cost)
			return true
		}
		held.Add(-cost)
		if !p.clicked || time.Now().After(deadline) {
			return false
		}
		select {
		case <-p.stop:
			return false
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// release gives back what p holds of the budget.
func (p *stickerPlayer) release(held *atomic.Int64) {
	held.Add(-p.cost.Swap(0))
}

// stop stops the player of key.
func (ps *players) stop(key string, p *stickerPlayer) {
	close(p.stop)
	delete(ps.m, key)
	if p.preview {
		ps.previews--
	}
	if p.fx {
		ps.effects--
	}
}

func (p *stickerPlayer) run(load func() []byte, maxSide int, held *atomic.Int64, invalidate func()) {
	data := load()
	if lottie.IsWAS(data) {
		p.runLottie(data, maxSide, held, invalidate)
		return
	}
	a, err := webpanim.Parse(data)
	if err != nil {
		return // the still first frame stays
	}
	w, h := a.Width, a.Height
	if s := max(w, h); s > maxSide {
		w, h = max(1, w*maxSide/s), max(1, h*maxSide/s)
	}
	if !p.reserve(held, webpCost(a.Width, a.Height, w, h)) {
		return
	}
	defer p.release(held)
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
		if !p.show(buf, due, invalidate) {
			return
		}
		if a.Loops > 0 && n+1 >= a.Loops*len(a.Frames) {
			return // played as often as it asks; the last frame stays
		}
		due = nextDue(due, delay)
	}
}

// lottieFPS caps how many frames a second a Lottie sticker is drawn at
// (Settings > Performance > Advanced, perf.lottieFPS): each one is rendered
// from its shapes, and 60 doubles the work of 30. Players read it on their
// own goroutines, as each starts.
var lottieFPS = func() *atomic.Int32 {
	var v atomic.Int32
	v.Store(int32(defaultPerf.lottieFPS))
	return &v
}()

// runLottie plays a Lottie sticker (WhatsApp's .was), drawing each frame
// from its shapes, as WhatsApp Web's lottie-web does. It loops until the
// sticker leaves the screen. maxSide is twice the size shown, as pictures
// are decoded for the image cache; a Lottie sticker is drawn at the size
// shown, sharp at any size.
func (p *stickerPlayer) runLottie(data []byte, maxSide int, held *atomic.Int64, invalidate func()) {
	a, err := lottie.Load(data)
	if err != nil {
		return
	}
	p.lottie.Store(true)
	w, h := a.Fit(max(1, maxSide/2))
	p.playLottie(a, w, h, true, held, invalidate)
}

// playLottie plays a at w x h, looping or once.
func (p *stickerPlayer) playLottie(a *lottie.Animation, w, h int, loop bool, held *atomic.Int64, invalidate func()) {
	// The three buffers and the renderer's coverage sums; what else the
	// renderer keeps (layers drawn apart for masks and mattes) is counted
	// after the first frame.
	if !p.reserve(held, lottieCost(w, h)) {
		return
	}
	defer p.release(held)
	step := max(1, math.Round(a.FrameRate/float64(max(1, lottieFPS.Load()))))
	delay := time.Duration(step / a.FrameRate * float64(time.Second))
	frames := float64(a.Frames())
	due := time.Now()
	counted := false
	for f := 0.0; ; f += step {
		if f >= frames {
			if !loop {
				break
			}
			f = 0
		}
		buf := p.free(w, h)
		a.Render(buf, f)
		if !counted {
			counted = true
			if extra := a.Scratch() - 4*int64(w)*int64(h); extra > 0 {
				held.Add(extra)
				p.cost.Add(extra)
			}
		}
		if !p.show(buf, due, invalidate) {
			return
		}
		due = nextDue(due, delay)
	}
	// Played once: the last frame stays its time.
	select {
	case <-p.stop:
	case <-time.After(time.Until(due)):
	}
}

// webpCost is what an animated WebP of a cw x ch canvas, drawn at w x h,
// holds: the canvas, a decoded frame and three buffers.
func webpCost(cw, ch, w, h int) int64 {
	return int64(cw)*int64(ch)*8 + 3*int64(w)*int64(h)*4
}

// lottieCost is what a Lottie sticker drawn at w x h holds at first: three
// buffers and the renderer's coverage sums.
func lottieCost(w, h int) int64 {
	return 4 * 4 * int64(w) * int64(h)
}

// show makes buf the newest frame at due. It returns false once the player
// is stopped.
func (p *stickerPlayer) show(buf *image.RGBA, due time.Time, invalidate func()) bool {
	select {
	case <-p.stop:
		return false
	case <-time.After(time.Until(due)):
	}
	p.mu.Lock()
	p.ready = p.index(buf)
	p.mu.Unlock()
	if invalidate != nil {
		invalidate()
	}
	return true
}

func nextDue(due time.Time, delay time.Duration) time.Time {
	due = due.Add(delay)
	if now := time.Now(); due.Before(now.Add(-time.Second)) {
		due = now // fell far behind (e.g. the machine slept)
	}
	return due
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
