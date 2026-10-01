package ui

import (
	"fmt"
	"image"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/video"
)

// controlsLinger is how long the video controls stay after the pointer stops.
const controlsLinger = 2500 * time.Millisecond

// videoView is the video playing in the media viewer.
type videoView struct {
	msgID    string
	player   *video.Player
	loading  bool // waiting for the download
	external bool // no player here: play opens the system's video player
	frame    paint.ImageOp
	size     image.Point
	muted    bool // kept from video to video
	lastMove time.Time
	overBar  bool // the pointer is on the controls
	ctrl     tween
	seek     struct {
		x, y, w  int // the track last frame: left end, middle, width
		dragging bool
		t        float32   // knob position while dragging
		sent     time.Time // last seek sent while dragging
		target   time.Duration
		until    time.Time // show target until the player gets there
	}
}

func isVideo(m *model.Message) bool { return m.Media == model.MediaVideo || m.Media == model.MediaGIF }

// syncVideo starts the video the viewer shows, and stops it when the
// viewer moves on to something else.
func (u *UI) syncVideo(m *model.Message) {
	vv := &u.viewer.video
	if m == nil || !isVideo(m) {
		if vv.msgID != "" {
			u.stopVideo()
		}
		return
	}
	if vv.msgID != m.ID {
		u.stopVideo()
		vv.msgID = m.ID
		u.loadVideo(vv, m)
	}
}

// loadVideo opens the video, or waits for its download.
func (u *UI) loadVideo(vv *videoView, m *model.Message) {
	path := u.backend.MediaFile(m)
	if path == "" {
		vv.loading = true // videoDownloaded continues
		return
	}
	vv.loading = false
	p, err := video.Open(path, u.images.invalidate)
	if err != nil {
		vv.external = true
		return
	}
	p.SetMuted(vv.muted)
	vv.player = p
}

// stop closes the video, keeping only the mute setting.
func (vv *videoView) stop() {
	if vv.player != nil {
		vv.player.Close()
	}
	*vv = videoView{muted: vv.muted}
}

// stopVideo closes the viewer's video.
func (u *UI) stopVideo() { u.viewer.video.stop() }

// videoDownloaded opens the viewer's or the status viewer's video once its
// download ends.
func (u *UI) videoDownloaded(e model.MediaEvent) {
	if vv := &u.viewer.video; vv.loading && vv.msgID == e.MsgID {
		vv.loading = false
		if e.Failed {
			return // the play button tries again
		}
		if m, _ := u.viewerMsg(u.viewerItems()); m != nil && m.ID == e.MsgID {
			u.loadVideo(vv, m)
		}
	}
	u.statusVideoDownloaded(e)
}

// toggleVideo plays or pauses the viewer's video.
func (u *UI) toggleVideo(gtx C, m *model.Message) {
	vv := &u.viewer.video
	vv.lastMove = gtx.Now
	switch {
	case vv.external:
		u.backend.OpenMedia(m)
		u.toast("Opening video…")
	case vv.player != nil:
		if st := vv.player.Status(); st.Paused || st.Ended {
			vv.player.Play()
		} else {
			vv.player.Pause()
		}
	case !vv.loading:
		u.loadVideo(vv, m) // after a failed download
	}
}

// videoFrame returns the video's current frame, fitted to at most max
// pixels, or nil before the first one.
func (u *UI) videoFrame(vv *videoView, max image.Point) *imgEntry {
	if vv.player == nil {
		return nil
	}
	if vv.player.Status().Err != nil {
		vv.player.Close()
		vv.player, vv.external = nil, true
		u.toast("This video can't play here. Press play to open it in your video player.")
		return nil
	}
	vv.player.SetFrameSize(max)
	if f, changed := vv.player.Frame(); changed {
		vv.frame, vv.size = paint.NewImageOp(f), f.Rect.Size()
	}
	if vv.size == (image.Point{}) {
		return nil
	}
	return &imgEntry{state: imgReady, op: vv.frame, size: vv.size}
}

// layoutVideoCenter draws the big play button, or a spinner while the
// video downloads or starts.
func (u *UI) layoutVideoCenter(gtx C, m *model.Message, c image.Point) {
	vv := &u.viewer.video
	var st video.Status
	if vv.player != nil {
		st = vv.player.Status()
	}
	rad := gtx.Dp(34)
	switch {
	case vv.loading || vv.player != nil && vv.size == (image.Point{}):
		s := gtx.Dp(44)
		fillCircle(gtx, c, rad, argb(0x000000, 0x90))
		t := op.Offset(c.Sub(image.Pt(s/2, s/2))).Push(gtx.Ops)
		lg := gtx
		lg.Constraints = layout.Exact(image.Pt(s, s))
		l := material.Loader(u.th)
		l.Color = rgb(0xffffff)
		l.Layout(lg)
		t.Pop()
	case vv.player == nil || st.Paused || st.Ended:
		t := op.Offset(c.Sub(image.Pt(rad, rad))).Push(gtx.Ops)
		cg := gtx
		cg.Constraints = layout.Exact(image.Pt(2*rad, 2*rad))
		cl := u.btn("vw:play")
		clickable(cg, cl, func(gtx C) D {
			bg := mix(argb(0x000000, 0x90), argb(0x000000, 0xc0), u.hover(gtx, cl))
			playButton(gtx, image.Pt(rad, rad), rad, bg)
			return D{Size: gtx.Constraints.Min}
		})
		t.Pop()
	}
}

// layoutVideoControls draws the bar along the bottom of the video, in r:
// play/pause, the time, a seek bar, the length and mute. It shows while
// the pointer moves over the video, and while paused.
func (u *UI) layoutVideoControls(gtx C, m *model.Message, r image.Rectangle) {
	vv := &u.viewer.video
	if vv.player == nil || vv.size == (image.Point{}) || r.Dx() < gtx.Dp(220) {
		return
	}
	st := vv.player.Status()
	dur := st.Dur
	if dur <= 0 {
		dur = time.Duration(m.Duration) * time.Second
	}

	// The seek bar: pressing jumps there, dragging follows the pointer.
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &vv.seek, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok || vv.seek.w <= 0 || dur <= 0 {
			continue
		}
		t := min(1, max(0, e.Position.X/float32(vv.seek.w)))
		switch e.Kind {
		case pointer.Press:
			vv.seek.dragging, vv.seek.t = true, t
			vv.player.Seek(time.Duration(float64(t)*float64(dur)), false)
			vv.seek.sent = gtx.Now
		case pointer.Drag:
			if !vv.seek.dragging {
				continue
			}
			vv.seek.t = t
			if gtx.Now.Sub(vv.seek.sent) > 60*time.Millisecond {
				vv.player.Seek(time.Duration(float64(t)*float64(dur)), false)
				vv.seek.sent = gtx.Now
			}
		case pointer.Release, pointer.Cancel:
			if vv.seek.dragging {
				vv.seek.target = time.Duration(float64(vv.seek.t) * float64(dur))
				vv.seek.until = gtx.Now.Add(1500 * time.Millisecond)
				vv.player.Seek(vv.seek.target, true)
			}
			vv.seek.dragging = false
		}
		vv.lastMove = gtx.Now
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &vv.overBar, Kinds: pointer.Move | pointer.Enter | pointer.Leave})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok {
			vv.overBar = e.Kind != pointer.Leave
			vv.lastMove = gtx.Now
		}
	}
	if u.btn("vw:vplay").Clicked(gtx) {
		u.toggleVideo(gtx, m)
		st = vv.player.Status()
	}
	if u.btn("vw:mute").Clicked(gtx) {
		vv.muted = !vv.muted
		vv.player.SetMuted(vv.muted)
	}

	playing := !st.Paused && !st.Ended
	show := !playing || vv.seek.dragging || vv.overBar || gtx.Now.Sub(vv.lastMove) < controlsLinger
	if playing && show && !vv.overBar && !vv.seek.dragging {
		gtx.Execute(op.InvalidateCmd{At: vv.lastMove.Add(controlsLinger)}) // to hide them
	}
	a := easeOut(vv.ctrl.step(gtx, show, durPopIn))
	if a == 0 {
		return
	}

	// Where the knob is: under the pointer while dragging, then at the
	// target until the player has caught up, so it never jumps back.
	pos := st.Pos
	switch {
	case vv.seek.dragging:
		pos = time.Duration(float64(vv.seek.t) * float64(dur))
	case gtx.Now.Before(vv.seek.until):
		if d := pos - vv.seek.target; d > 300*time.Millisecond || d < -300*time.Millisecond {
			pos = vv.seek.target
			gtx.Execute(op.InvalidateCmd{At: vv.seek.until})
		} else {
			vv.seek.until = time.Time{}
		}
	}
	var f float32
	if dur > 0 {
		f = min(1, max(0, float32(pos)/float32(dur)))
	}

	barH := gtx.Dp(58)
	bar := image.Rect(r.Min.X, r.Max.Y-barH, r.Max.X, r.Max.Y)
	white := rgb(0xffffff)
	withOpacity(gtx, a, func() {
		func() {
			defer clip.Rect(bar).Push(gtx.Ops).Pop()
			event.Op(gtx.Ops, &vv.overBar)
			paint.LinearGradientOp{
				Stop1: f32.Pt(0, float32(bar.Min.Y)), Color1: argb(0x000000, 0),
				Stop2: f32.Pt(0, float32(bar.Max.Y)), Color2: argb(0x000000, 0xb0),
			}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
		}()
		cy := bar.Max.Y - gtx.Dp(24)
		btn := func(key string, x int, ic func() D) {
			s := gtx.Dp(36)
			t := op.Offset(image.Pt(x, cy-s/2)).Push(gtx.Ops)
			cg := gtx
			cg.Constraints = layout.Exact(image.Pt(s, s))
			cl := u.btn(key)
			clickable(cg, cl, func(gtx C) D {
				if h := u.hover(gtx, cl); h > 0 {
					fillCircle(gtx, image.Pt(s/2, s/2), s/2, argb(0xffffff, uint8(0x30*h)))
				}
				return centerIn(gtx, s, func(gtx C) D { return ic() })
			})
			t.Pop()
		}
		pad := gtx.Dp(8)
		playIc := icPauseFill
		if !playing {
			playIc = icPlayFill
		}
		btn("vw:vplay", r.Min.X+pad, func() D { return drawIcon(gtx, playIc, 26, white) })
		muteIc := icVolumeFill
		if vv.muted {
			muteIc = icVolumeOffFill
		}
		btn("vw:mute", r.Max.X-pad-gtx.Dp(36), func() D { return drawIcon(gtx, muteIc, 24, white) })

		lg := gtx
		lg.Constraints = layout.Constraints{Max: r.Size()}
		cur := record(lg, u.label(13, fmtClock(pos), white, labelOpts{maxLines: 1}).Layout)
		total := record(lg, u.label(13, fmtClock(dur), white, labelOpts{maxLines: 1}).Layout)
		x0 := r.Min.X + pad + gtx.Dp(36) + gtx.Dp(6)
		cur.at(gtx, x0, cy-cur.size.Y/2)
		x1 := r.Max.X - pad - gtx.Dp(36) - gtx.Dp(6) - total.size.X
		total.at(gtx, x1, cy-total.size.Y/2)

		// The track, with a tall hit area so it's easy to grab.
		track := image.Rectangle{Min: image.Pt(x0+cur.size.X+gtx.Dp(14), cy), Max: image.Pt(x1-gtx.Dp(14), cy)}
		vv.seek.x, vv.seek.y, vv.seek.w = track.Min.X, cy, track.Dx()
		if vv.seek.w <= 0 {
			return
		}
		hit := gtx.Dp(12)
		func() {
			defer op.Offset(image.Pt(track.Min.X, cy-hit)).Push(gtx.Ops).Pop()
			defer clip.Rect{Max: image.Pt(track.Dx(), 2*hit)}.Push(gtx.Ops).Pop()
			pointer.CursorPointer.Add(gtx.Ops)
			event.Op(gtx.Ops, &vv.seek)
		}()
		th := gtx.Dp(4)
		fillRRect(gtx, image.Rect(track.Min.X, cy-th/2, track.Max.X, cy+th/2), th/2, argb(0xffffff, 0x50))
		kx := track.Min.X + int(f*float32(track.Dx())+0.5)
		fillRRect(gtx, image.Rect(track.Min.X, cy-th/2, kx, cy+th/2), th/2, white)
		kr := gtx.Dp(6)
		if vv.seek.dragging || vv.overBar {
			kr = gtx.Dp(7)
		}
		fillCircle(gtx, image.Pt(kx, cy), kr, white)
	})
}

// fmtClock formats a video time as m:ss, or h:mm:ss.
func fmtClock(d time.Duration) string {
	s := int(d / time.Second)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
