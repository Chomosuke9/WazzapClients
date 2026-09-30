package ui

import (
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/widget"
)

// Animations. Gio has no animation system: every frame works out how far
// an animation has got from the frame time (gtx.Now), and an animation that
// is still moving asks for the next frame. Nothing asks for frames at rest,
// so an idle window still draws nothing.
//
// Never animate the color of an icon or a hand-drawn glyph: both are cached
// per color (internal/ui/icon, cachedGlyph), so every in-between color would
// be rasterized and kept. Fade them with withOpacity or pushFx instead.
// Colors of fills and text are free to animate.
//
// Keep opacity layers small. Gio draws each into an offscreen texture and
// keeps that texture, at the largest size ever needed, until the window
// closes: one fade of the whole window would pin ~16 MB for good. Fade a
// big area's backdrop color instead, and fade its parts one by one, or
// cover content on a plain background with a veil of that background.

// Durations, close to WhatsApp Desktop's.
const (
	durHoverIn  = 90 * time.Millisecond
	durHoverOut = 160 * time.Millisecond
	durPopIn    = 160 * time.Millisecond // menus and pickers opening
	durPopOut   = 110 * time.Millisecond // and closing
	durDialog   = 180 * time.Millisecond
	durPanel    = 240 * time.Millisecond // the info panel
	durGrow     = 180 * time.Millisecond // the reply preview, select mode
	durSwitch   = 150 * time.Millisecond // selection highlights
	durSlide    = 220 * time.Millisecond // tab underlines, chat list order
	durAppear   = 220 * time.Millisecond // new messages
	durScroll   = 360 * time.Millisecond // jumping to a message
)

// popDur is how long a popup takes to open (on) or close.
func popDur(on bool) time.Duration {
	if on {
		return durPopIn
	}
	return durPopOut
}

// tween is a progress from 0 (off) to 1 (on). It moves toward its target
// at a constant rate, taking dur for the whole way, so a change of target
// midway turns it around where it is and quick toggles never jump.
type tween struct {
	v  float32
	on bool
	at time.Time // frame time of the last step
}

// step moves t toward on for this frame and returns its linear progress.
// It asks for another frame while it moves. Step with an enabled context:
// a disabled one can't ask for frames.
func (t *tween) step(gtx C, on bool, dur time.Duration) float32 {
	target := float32(0)
	if on {
		target = 1
	}
	now := gtx.Now
	if now.IsZero() || dur <= 0 {
		// No clock (tests): jump.
		t.v, t.on = target, on
		return t.v
	}
	if on != t.on || t.v == target {
		t.at = now // starting, or at rest: count from this frame
	}
	t.on = on
	if t.v == target {
		return t.v
	}
	d := float32(now.Sub(t.at)) / float32(dur)
	t.at = now
	if on {
		t.v = min(1, t.v+d)
	} else {
		t.v = max(0, t.v-d)
	}
	if t.v != target {
		gtx.Execute(op.InvalidateCmd{})
	}
	return t.v
}

// snap ends t at on, without animating.
func (t *tween) snap(on bool) {
	t.on, t.v = on, 0
	if on {
		t.v = 1
	}
}

// follower is a number that glides to a new target when it changes, such
// as the position of a tab underline.
type follower struct {
	from, to, v float32
	start       time.Time
	set         bool
}

// step returns the value for this frame, gliding toward target over dur.
func (f *follower) step(gtx C, target float32, dur time.Duration) float32 {
	if !f.set || gtx.Now.IsZero() {
		f.snap(target)
		return target
	}
	if target != f.to {
		f.from, f.to, f.start = f.v, target, gtx.Now
	}
	if f.v == f.to {
		return f.v
	}
	p := float32(gtx.Now.Sub(f.start)) / float32(dur)
	if p >= 1 {
		f.v = f.to
	} else {
		f.v = lerp(f.from, f.to, easeOut(p))
		gtx.Execute(op.InvalidateCmd{})
	}
	return f.v
}

// snap jumps to v.
func (f *follower) snap(v float32) { f.from, f.to, f.v, f.set = v, v, v, true }

// switcher moves a highlight between items: the item selected now fades
// in while the previous one fades out. Items that scroll into view show
// their current state, which a per-item fade would animate again.
type switcher[K comparable] struct {
	cur, prev K
	prevV     float32 // the previous item's highlight when the switch began
	t         tween
	set       bool
}

// step selects k for this frame.
func (s *switcher[K]) step(gtx C, k K, dur time.Duration) {
	if !s.set {
		s.cur, s.set = k, true
		s.t.snap(true)
	}
	if k != s.cur {
		s.prev, s.prevV, s.cur = s.cur, smooth(s.t.v), k
		s.t.snap(false)
	}
	s.t.step(gtx, true, dur)
}

// of returns k's highlight, from 0 to 1.
func (s *switcher[K]) of(k K) float32 {
	switch k {
	case s.cur:
		return smooth(s.t.v)
	case s.prev:
		return s.prevV * (1 - smooth(s.t.v))
	}
	return 0
}

// Keyed fades, for things with many instances (rows, buttons) that each
// fade on their own. An entry exists only while its fade is on or moving,
// and is dropped after a frame it isn't drawn in.

type animTag uint8

const (
	tagHover  animTag = iota
	tagShow           // something shown on hover, like a chevron
	tagAppear         // a new message sliding in
	tagPop            // a reaction popping in
)

type animKey struct {
	p   any // a widget's address
	id  string
	tag animTag
}

type animEntry struct {
	tween
	seen int64 // frame of last use
}

type animStore struct {
	m     map[animKey]*animEntry
	frame int64
}

// fade returns k's linear progress toward on.
func (s *animStore) fade(gtx C, k animKey, on bool, in, out time.Duration) float32 {
	e := s.m[k]
	if e == nil {
		if !on {
			return 0
		}
		if s.m == nil {
			s.m = make(map[animKey]*animEntry)
		}
		e = new(animEntry)
		s.m[k] = e
	}
	e.seen = s.frame
	dur := out
	if on {
		dur = in
	}
	v := e.step(gtx, on, dur)
	if v == 0 && !on {
		delete(s.m, k)
	}
	return v
}

// start begins a fade of k from 0, even if one is running.
func (s *animStore) start(k animKey) {
	if s.m == nil {
		s.m = make(map[animKey]*animEntry)
	}
	s.m[k] = &animEntry{seen: s.frame}
}

// running reports whether k is fading.
func (s *animStore) running(k animKey) bool { return s.m[k] != nil }

// stop drops k, for fades that end on.
func (s *animStore) stop(k animKey) { delete(s.m, k) }

// endFrame drops the fades that weren't drawn this frame.
func (s *animStore) endFrame() {
	for k, e := range s.m {
		if e.seen != s.frame {
			delete(s.m, k)
		}
	}
	s.frame++
}

// hover returns c's hover highlight, eased, from 0 to 1.
func (u *UI) hover(gtx C, c *widget.Clickable) float32 {
	return u.hoverOn(gtx, c, c.Hovered())
}

// hoverOn is hover for a widget that counts as hovered through other means
// too.
func (u *UI) hoverOn(gtx C, c *widget.Clickable, on bool) float32 {
	return smooth(u.anims.fade(gtx, animKey{p: c, tag: tagHover}, on, durHoverIn, durHoverOut))
}

// Easing curves map linear progress to motion.

// easeOut starts fast and settles (cubic). Played backward, for closing,
// it accelerates away.
func easeOut(t float32) float32 {
	t = 1 - t
	return 1 - t*t*t
}

// smooth eases both ends, for fades.
func smooth(t float32) float32 { return t * t * (3 - 2*t) }

// easeInOut eases both ends more strongly (cubic), for scrolling.
func easeInOut(t float32) float32 {
	if t < 0.5 {
		return 4 * t * t * t
	}
	t = 2 - 2*t
	return 1 - t*t*t/2
}

// easeOutBack overshoots a little before settling, for pops.
func easeOutBack(t float32) float32 {
	const c1 = 1.70158
	const c3 = c1 + 1
	t--
	return 1 + c3*t*t*t + c1*t*t
}

func lerp(a, b, t float32) float32 { return a + (b-a)*t }

// lerpInt interpolates between two sizes in px.
func lerpInt(a, b int, t float32) int { return a + int(float32(b-a)*t+0.5) }

// faded scales a color's opacity by a.
func faded(c color.NRGBA, a float32) color.NRGBA {
	c.A = uint8(float32(c.A)*a + 0.5)
	return c
}

// withOpacity draws with opacity a, through a layer only when it's
// translucent. Nothing is drawn at 0.
func withOpacity(gtx C, a float32, draw func()) {
	if a <= 0 {
		return
	}
	if a < 1 {
		defer paint.PushOpacity(gtx.Ops, a).Pop()
	}
	draw()
}

// fadeW lays out w with opacity a.
func fadeW(gtx C, a float32, w layout.Widget) D {
	if a >= 1 {
		return w(gtx)
	}
	defer paint.PushOpacity(gtx.Ops, max(a, 0)).Pop()
	return w(gtx)
}

// fxDepth counts the transforms pushFx has pushed that aren't whole-pixel
// translations, so drawing code can tell when it may not be pixel aligned.
// Only the window goroutine draws.
var fxDepth int

// fxStack is a transform and an opacity pushed together; see pushFx.
type fxStack struct {
	t          op.TransformStack
	o          paint.OpacityStack
	hasT, hasO bool
	scaled     bool // not a whole-pixel translation; see fxDepth
}

// pushFx transforms and fades what is drawn until Pop. The identity and
// full opacity push nothing.
func pushFx(gtx C, opacity float32, tr f32.Affine2D) fxStack {
	var s fxStack
	if tr != (f32.Affine2D{}) {
		s.t, s.hasT = op.Affine(tr).Push(gtx.Ops), true
		if sx, hx, ox, hy, sy, oy := tr.Elems(); sx != 1 || hx != 0 || hy != 0 || sy != 1 || ox != float32(int(ox)) || oy != float32(int(oy)) {
			s.scaled = true
			fxDepth++
		}
	}
	if opacity < 1 {
		s.o, s.hasO = paint.PushOpacity(gtx.Ops, max(opacity, 0)), true
	}
	return s
}

func (s fxStack) Pop() {
	if s.hasO {
		s.o.Pop()
	}
	if s.hasT {
		s.t.Pop()
	}
	if s.scaled {
		fxDepth--
	}
}

// scaleAt scales by s around origin.
func scaleAt(origin image.Point, s float32) f32.Affine2D {
	if s == 1 {
		return f32.Affine2D{}
	}
	return f32.AffineId().Scale(pointF(origin), f32.Pt(s, s))
}

// moveBy translates by d px, rounded to whole pixels. Under a fractional
// offset Gio stencils every clip rectangle as a path, and rebuilds text
// outlines, which is slow and grows its coverage texture for good.
func moveBy(dx, dy float32) f32.Affine2D {
	return f32.AffineId().Offset(f32.Pt(float32(math.Round(float64(dx))), float32(math.Round(float64(dy)))))
}

// pushPopup animates a popup at linear progress v: it grows from 90% out
// of origin (the corner it's anchored to) and fades in.
func pushPopup(gtx C, v float32, origin image.Point) fxStack {
	e := easeOut(v)
	return pushFx(gtx, e, scaleAt(origin, lerp(0.9, 1, e)))
}

// veil fades what is drawn in r, on a plain background col, in from that
// background (v from 0 to 1) without an opacity layer.
func (u *UI) veil(gtx C, r image.Rectangle, col color.NRGBA, v float32) {
	if v < 1 {
		fillRect(gtx, r, faded(col, 1-v))
	}
}
