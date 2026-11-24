package ui

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Stickers with a sound (Message.Sound) carry a round speaker button in
// their corner, as on phones. They play silently, as any sticker does,
// until it's clicked: then the sound plays once, the animation starting
// over with it, and a second click stops it. It's the voice messages'
// player (voiceState), so only one of them all sounds at a time.

// The speaker's size and its distance from the sticker's corner, in parts
// of the sticker's side, and its icon's size, measured on a phone.
const (
	soundBadge = 0.255
	soundInset = 0.05
	soundIcon  = 0.125
)

// toggleStickerSound plays a sticker's sound from its start, or stops it.
func (u *UI) toggleStickerSound(m *model.Message) {
	on := u.voice.key == fileKey(m)
	u.stopRoundSound()
	u.stopVoice()
	if on {
		return
	}
	u.voice = voiceState{key: fileKey(m), msg: m, seekTo: -1}
	u.loadVoice()
}

// endStickerSound closes the player of a sticker's sound once it has
// played, though the sticker may have left the screen meanwhile.
func (u *UI) endStickerSound() {
	if v := &u.voice; v.player != nil && v.msg.Media == model.MediaSticker && v.player.Status().Ended {
		u.stopVoice()
	}
}

// stickerSound draws the speaker of a sticker shown sz px wide.
func (u *UI) stickerSound(gtx C, m *model.Message, sz int, animated bool) {
	btn := u.btn("stksound:" + m.ID)
	if btn.Clicked(gtx) && !u.conv.selecting {
		u.toggleStickerSound(m)
	}
	_, _, _, loading := u.voiceProgress(m)
	v := &u.voice
	on := v.key == fileKey(m)
	if on && v.player != nil && !loading && !v.synced {
		v.synced = true
		if animated {
			u.playSticker(m, sz)
		}
	}
	side := int(float32(sz) * soundBadge)
	at := int(float32(sz) * soundInset)
	defer op.Offset(image.Pt(at, at)).Push(gtx.Ops).Pop()
	draw := func(gtx C) D {
		// A phone's 45% of this color; Gio blends in linear space.
		fillCircle(gtx, image.Pt(side/2, side/2), side/2, argb(0x0b141a, 0xa8+uint8(0x20*u.hover(gtx, btn))))
		white := rgb(0xffffff)
		if on && loading {
			lg := gtx
			s := side * 6 / 10
			lg.Constraints = layout.Exact(image.Pt(s, s))
			t := op.Offset(image.Pt((side-s)/2, (side-s)/2)).Push(gtx.Ops)
			l := material.Loader(u.th)
			l.Color = white
			l.Layout(lg)
			t.Pop()
			return D{Size: image.Pt(side, side)}
		}
		ic := icVolumeOff
		if on {
			ic = icVolume
		}
		centerIn(gtx, side, iconW(ic, dp(gtx, int(float32(sz)*soundIcon)), white))
		return D{Size: image.Pt(side, side)}
	}
	cg := gtx
	cg.Constraints = layout.Exact(image.Pt(side, side))
	if u.conv.selecting {
		draw(cg) // a click picks the message
		return
	}
	clickable(cg, btn, draw)
}
