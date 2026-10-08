package ui

import (
	"image"
	"strconv"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/chomosuke9/wazzapclients/internal/video"
)

// The volume of videos, voice messages and audio files, set with the
// slider beside a video's mute button. It's an app preference, in percent,
// so a loud video doesn't start at full volume again after a restart.
const prefVolume = "volume" // 100 if ""

// volumeStep is how much a mouse wheel notch over the slider moves it.
const volumeStep = 0.05

// volSlider is a volume slider's input state.
type volSlider struct {
	w        int // the track's width last frame
	dragging bool
}

// loadVolume reads the stored volume.
func (u *UI) loadVolume() {
	u.volume = 1
	if v, err := strconv.Atoi(u.backend.Pref(prefVolume)); err == nil && v >= 0 && v <= 100 {
		u.volume = float32(v) / 100
	}
}

// setVolume changes the volume of everything playing. save remembers it,
// which a drag does only once it ends.
func (u *UI) setVolume(v float32, save bool) {
	v = min(1, max(0, v))
	if v != u.volume {
		u.volume = v
		for _, p := range []*video.Player{u.viewer.video.player, u.status.viewer.video.player, u.voice.player} {
			if p != nil {
				p.SetVolume(float64(v))
			}
		}
	}
	if save {
		s := strconv.Itoa(int(v*100 + 0.5))
		if s == "100" {
			s = ""
		}
		u.backend.SetPref(prefVolume, s)
	}
}

// soundOn reports whether a player muted or not would be heard.
func (u *UI) soundOn(muted bool) bool { return !muted && u.volume > 0 }

// toggleMute is vv's mute button: it mutes what you hear, or unmutes,
// turning a volume of 0 back up so that you hear it.
func (u *UI) toggleMute(vv *videoView) {
	vv.muted = u.soundOn(vv.muted)
	if !vv.muted && u.volume == 0 {
		u.setVolume(0.5, true)
	}
	if vv.player != nil {
		vv.player.SetMuted(vv.muted)
	}
}

// layoutVolume draws vv's volume slider w px wide, its track centered on
// the gtx origin's row, and reports whether the user moved it. Moving it
// unmutes vv.
func (u *UI) layoutVolume(gtx C, vv *videoView, w int) bool {
	s := &vv.vol
	moved := false
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: s, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Scroll,
			ScrollY: pointer.ScrollRange{Min: -1e6, Max: 1e6}})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok || s.w <= 0 {
			continue
		}
		t := min(1, max(0, e.Position.X/float32(s.w)))
		switch e.Kind {
		case pointer.Press:
			s.dragging = true
			u.setVolume(t, false)
		case pointer.Drag:
			if !s.dragging {
				continue
			}
			u.setVolume(t, false)
		case pointer.Release, pointer.Cancel:
			if s.dragging {
				u.setVolume(u.volume, true)
			}
			s.dragging = false
		case pointer.Scroll:
			if e.Scroll.Y == 0 {
				continue
			}
			d := float32(volumeStep)
			if e.Scroll.Y > 0 {
				d = -d
			}
			u.setVolume(u.volume+d, true)
		}
		moved = true
		if vv.muted && u.volume > 0 {
			vv.muted = false
			if vv.player != nil {
				vv.player.SetMuted(false)
			}
		}
	}
	s.w = w
	if w <= 0 {
		return moved
	}

	white := rgb(0xffffff)
	hit := gtx.Dp(12)
	func() {
		defer op.Offset(image.Pt(0, -hit)).Push(gtx.Ops).Pop()
		defer clip.Rect{Max: image.Pt(w, 2*hit)}.Push(gtx.Ops).Pop()
		pointer.CursorPointer.Add(gtx.Ops)
		event.Op(gtx.Ops, s)
	}()
	f := u.volume
	if vv.muted {
		f = 0
	}
	th := gtx.Dp(4)
	fillRRect(gtx, image.Rect(0, -th/2, w, th-th/2), th/2, argb(0xffffff, 0x50))
	kx := int(f*float32(w) + 0.5)
	fillRRect(gtx, image.Rect(0, -th/2, kx, th-th/2), th/2, white)
	kr := gtx.Dp(6)
	if s.dragging {
		kr = gtx.Dp(7)
	}
	fillCircle(gtx, image.Pt(kx, 0), kr, white)
	return moved
}
