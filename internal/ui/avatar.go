package ui

import (
	"image"

	"gioui.org/op/clip"
	"gioui.org/unit"
)

// avatarPx is the resolution profile pictures are decoded at; WhatsApp's
// preview pictures are about this size anyway.
const avatarPx = 160

// avatar draws a round profile picture for id (a chat or user JID), or the
// default person/group avatar while there is none.
func (u *UI) avatar(gtx C, id, name string, group bool, size unit.Dp) D {
	p := u.pal
	px := gtx.Dp(size)
	r := image.Rect(0, 0, px, px)
	dims := D{Size: r.Size()}
	if id != "" {
		b := u.backend
		if e := u.images.get("a:"+id, avatarPx, func() []byte { return b.Avatar(id) }); e.state == imgReady {
			defer clip.Ellipse(r).Push(gtx.Ops).Pop()
			paintCover(gtx, e.op, e.size, r)
			return dims
		}
	}
	bg, fg, ic, scale := p.UserAvatar, p.UserAvatarIcon, icPerson, float32(0.62)
	if group {
		bg, fg, ic, scale = p.GroupAvatar, p.GroupAvatarIcon, icGroup, 0.5
	}
	fillCircle(gtx, image.Pt(px/2, px/2), px/2, bg)
	centerIn(gtx, px, iconW(ic, size*unit.Dp(scale), fg))
	return dims
}
