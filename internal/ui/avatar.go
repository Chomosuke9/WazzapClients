package ui

import (
	"image"

	"gioui.org/op/clip"
	"gioui.org/unit"
)

// avatarPx is the resolution profile pictures are decoded at; WhatsApp's
// preview pictures are about this size anyway.
const avatarPx = 160

type avatarKind int

const (
	avatarPerson avatarKind = iota
	avatarGroup
	avatarChannel
	avatarCommunity // rounded square
)

// avatar draws a round profile picture for id (a chat or user JID), or the
// default person/group avatar while there is none.
func (u *UI) avatar(gtx C, id, name string, group bool, size unit.Dp) D {
	kind := avatarPerson
	if group {
		kind = avatarGroup
	}
	return u.avatarOf(gtx, id, kind, size)
}

// avatarImage returns id's decoded profile picture, or nil while there is none.
func (u *UI) avatarImage(id string) *imgEntry {
	b := u.backend
	if e := u.images.get("a:"+id, avatarPx, func() []byte { return b.Avatar(id) }); e.state == imgReady {
		return e
	}
	return nil
}

// avatarOf draws the picture of id in the shape and with the placeholder
// that suit its kind.
func (u *UI) avatarOf(gtx C, id string, kind avatarKind, size unit.Dp) D {
	p := u.pal
	px := gtx.Dp(size)
	r := image.Rect(0, 0, px, px)
	dims := D{Size: r.Size()}
	radius := px / 2
	if kind == avatarCommunity {
		radius = px * 10 / 52
	}
	if id != "" {
		b := u.backend
		if e := u.images.get("a:"+id, avatarPx, func() []byte { return b.Avatar(id) }); e.state == imgReady {
			defer clip.UniformRRect(r, radius).Push(gtx.Ops).Pop()
			paintCover(gtx, e.op, e.size, r)
			return dims
		}
	}
	switch kind {
	case avatarChannel:
		fillCircle(gtx, image.Pt(px/2, px/2), px/2, p.ChannelAvatar)
		centerIn(gtx, px, func(gtx C) D {
			return channelsIcon(gtx, size*0.5, p.ChannelAvatarIcon, p.ChannelAvatar, true)
		})
		return dims
	case avatarCommunity:
		fillRRect(gtx, r, radius, p.GroupAvatar)
		centerIn(gtx, px, iconW(icGroupsFill, size*0.55, p.GroupAvatarIcon))
		return dims
	}
	bg, fg, ic, scale := p.UserAvatar, p.UserAvatarIcon, icPerson, float32(0.62)
	if kind == avatarGroup {
		bg, fg, ic, scale = p.GroupAvatar, p.GroupAvatarIcon, icGroup, 0.5
	}
	fillCircle(gtx, image.Pt(px/2, px/2), px/2, bg)
	centerIn(gtx, px, iconW(ic, size*unit.Dp(scale), fg))
	return dims
}
