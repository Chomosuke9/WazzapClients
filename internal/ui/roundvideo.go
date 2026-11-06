package ui

import (
	"image"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/video"
)

// roundVideoDp is how wide a video note (a ptvMessage) is drawn.
const roundVideoDp = 240

// isRound reports whether m is drawn as a video note. A view once one stays
// a view once card.
func isRound(m *model.Message) bool { return m.Round && m.Kind == model.KindImage }

// gifVideo returns the player of the GIF or video note key, nil while it
// opens or when it isn't on screen.
func (ps *players) gifVideo(key string) *video.Player {
	g := ps.gifs[key]
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.p
}

// toggleRound starts a video note from its start with sound, or pauses or
// resumes the one playing with sound. Only one plays with sound at a time.
func (u *UI) toggleRound(m *model.Message) {
	ps := &u.players
	key := fileKey(m)
	if ps.sound == key {
		if p := ps.gifVideo(key); p != nil {
			if p.Status().Paused {
				p.Play()
			} else {
				p.Pause()
			}
		}
		return
	}
	u.stopRoundSound()
	u.stopVoice()
	ps.sound, ps.soundOn, ps.soundUsed = key, false, ps.frame
}

// stopRoundSound takes the video note playing with sound back to where it
// was: muted and looping, if it goes on playing at all.
func (u *UI) stopRoundSound() {
	ps := &u.players
	if ps.sound == "" {
		return
	}
	if p := ps.gifVideo(ps.sound); p != nil {
		p.SetMuted(true)
		p.SetLoop(true)
		p.Seek(0, false)
		p.Play()
	}
	ps.sound, ps.soundOn = "", false
}

// roundAutoplays reports whether a video note plays by itself, muted and
// looping: like a GIF once its file is here. The file comes as Settings >
// Storage says for videos, or the first time it's clicked.
func (u *UI) roundAutoplays(m *model.Message) bool {
	return u.autoplays(m) && u.fileReady(m)
}

// roundImage returns the picture of a video note: its current frame while
// it plays, or else its thumbnail.
func (u *UI) roundImage(m *model.Message, sz int) *imgEntry {
	img := u.messageImage(m, sz)
	if u.blurred() {
		return img
	}
	ps := &u.players
	key := fileKey(m)
	sounding := ps.sound == key
	if sounding {
		ps.soundUsed = ps.frame
	} else if !u.roundAutoplays(m) {
		return img
	}
	f := u.gifFrame(m, sz)
	if sounding {
		u.syncRoundSound(m, key)
	}
	if f != nil {
		return f
	}
	return img
}

// syncRoundSound moves the player of the video note playing with sound
// along: to its start with sound once it opened, and back to muted when it
// ends. A file that can't play here goes to the system's player.
func (u *UI) syncRoundSound(m *model.Message, key string) {
	ps := &u.players
	if ps.gifFetch[key] == gifFailed {
		ps.sound, ps.soundOn = "", false
		if u.fileReady(m) {
			u.backend.OpenMedia(m)
		}
		return
	}
	p := ps.gifVideo(key)
	switch {
	case p == nil:
	case !ps.soundOn:
		p.SetLoop(false)
		p.SetVolume(gain(u.volume))
		p.SetMuted(false)
		p.Seek(0, true)
		p.Play()
		ps.soundOn = true
	case p.Status().Ended:
		u.stopRoundSound()
	}
}

// roundPicture draws a video note in a circle sz across. A click plays it
// with sound.
func (u *UI) roundPicture(gtx C, m *model.Message, sz int) {
	ps := &u.players
	key := fileKey(m)
	cl := u.btn("round:" + m.ID)
	size := image.Pt(sz, sz)
	if cl.Clicked(gtx) {
		if ps.gifFetch[key] == gifFailed {
			// Its file didn't come, or can't play: the viewer has what's left.
			u.openViewer(m)
			u.viewer.origin = u.viewerOrigin(gtx, cl, size)
		} else {
			u.toggleRound(m)
		}
	}
	img := u.roundImage(m, sz)
	r := image.Rectangle{Max: size}
	func() {
		defer roundShape(sz, sz, sz/2).Push(gtx.Ops).Pop()
		switch {
		case img != nil && img.state == imgReady:
			paintCover(gtx, img.op, img.size, r)
		case m.ImageA != 0 || m.ImageB != 0:
			u.gradientImage(gtx, r, m.ImageA, m.ImageB)
		default:
			fillRect(gtx, r, u.pal.Hover)
		}
	}()

	sounding := ps.sound == key
	live := img != nil && img.live
	var st video.Status
	if p := ps.gifVideo(key); sounding && p != nil {
		st = p.Status()
	}
	mid := image.Pt(sz/2, sz/2)
	dark := argb(0x000000, 0x80)
	switch {
	case sounding && !live:
		rad := gtx.Dp(26)
		fillCircle(gtx, mid, rad, dark)
		s := gtx.Dp(34)
		t := op.Offset(mid.Sub(image.Pt(s/2, s/2))).Push(gtx.Ops)
		lg := gtx
		lg.Constraints = layout.Exact(image.Pt(s, s))
		l := material.Loader(u.th)
		l.Color = rgb(0xffffff)
		l.Layout(lg)
		t.Pop()
	case !live || st.Paused:
		playButton(gtx, mid, gtx.Dp(26), dark)
	}
	if sounding && st.Dur > 0 {
		f := min(1, max(0, float32(st.Pos)/float32(st.Dur)))
		if f > 0 {
			w := float32(gtx.Dp(4))
			c := f32.Pt(float32(sz)/2, float32(sz)/2)
			strokeArc(gtx, c, float32(sz)/2-w/2, -math.Pi/2, f*2*math.Pi, w, u.pal.Green)
		}
	}
	u.roundChip(gtx, m, st, sounding, live, sz)

	cg := gtx
	cg.Constraints = layout.Exact(size)
	clickable(cg, cl, func(gtx C) D { return D{Size: size} })
}

// roundChip draws the time left on a chip at the bottom of a video note,
// with a crossed out speaker while it plays muted.
func (u *UI) roundChip(gtx C, m *model.Message, st video.Status, sounding, live bool, sz int) {
	secs := m.Duration
	if sounding && st.Dur > 0 {
		secs = int((st.Dur - st.Pos).Seconds() + 0.5)
	}
	muted := live && !sounding
	if secs <= 0 && !muted {
		return
	}
	white := rgb(0xffffff)
	lg := gtx
	lg.Constraints = layout.Constraints{Max: image.Pt(sz, sz)}
	txt := record(lg, u.label(11.5, fmtClock(time.Duration(secs)*time.Second), white, labelOpts{maxLines: 1}).Layout)
	isz, gap := 0, 0
	if muted {
		isz, gap = gtx.Dp(14), gtx.Dp(3)
	}
	padX, padY := gtx.Dp(8), gtx.Dp(3)
	w, h := txt.size.X+isz+gap+2*padX, max(txt.size.Y, isz)+2*padY
	x, y := (sz-w)/2, sz-h-gtx.Dp(10)
	fillRRect(gtx, image.Rect(x, y, x+w, y+h), h/2, argb(0x000000, 0x80))
	if muted {
		t := op.Offset(image.Pt(x+padX, y+(h-isz)/2)).Push(gtx.Ops)
		drawIcon(gtx, icVolumeOffFill, 14, white)
		t.Pop()
	}
	txt.at(gtx, x+padX+isz+gap, y+(h-txt.size.Y)/2)
}
