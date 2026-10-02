package ui

import (
	"hash/fnv"
	"image"
	"strings"
	"unicode"

	"gioui.org/font"
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

// avatar draws a round profile picture for id (a chat or user JID), or
// while there is none, like WhatsApp: a person's initial on a color of
// their own, or the group placeholder.
func (u *UI) avatar(gtx C, id, name string, group bool, size unit.Dp) D {
	kind := avatarPerson
	if group {
		kind = avatarGroup
	}
	return u.drawAvatar(gtx, id, name, kind, size)
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
	return u.drawAvatar(gtx, id, "", kind, size)
}

func (u *UI) drawAvatar(gtx C, id, name string, kind avatarKind, size unit.Dp) D {
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
			defer roundShape(px, px, radius).Push(gtx.Ops).Pop()
			paintCover(gtx, e.op, e.size, r)
			return dims
		}
	}
	mid := image.Pt(px/2, px/2)
	switch kind {
	case avatarChannel:
		fillCircle(gtx, mid, px/2, p.ChannelAvatar)
		centerIn(gtx, px, func(gtx C) D {
			return channelsIcon(gtx, size*0.5, p.ChannelAvatarIcon, p.ChannelAvatar, true)
		})
		return dims
	case avatarCommunity:
		fillRRect(gtx, r, radius, p.GroupAvatar)
		centerIn(gtx, px, iconW(icGroupsFill, size*0.55, p.GroupAvatarIcon))
		return dims
	case avatarGroup:
		if c := u.chatByID(id); c != nil && c.General {
			// A community's General chat: a speech bubble.
			fillCircle(gtx, mid, px/2, p.GeneralAvatar)
			centerIn(gtx, px, u.label(unit.Sp(float32(size)*0.42), "💬", p.Text).Layout)
			return dims
		}
		fillCircle(gtx, mid, px/2, p.GroupAvatar)
		centerIn(gtx, px, iconW(icGroup, size*0.5, p.GroupAvatarIcon))
		return dims
	}
	if initial := avatarInitial(name); initial != "" && len(p.AvatarBgs) > 0 {
		i := avatarTint(id, name, len(p.AvatarBgs))
		fillCircle(gtx, mid, px/2, p.AvatarBgs[i])
		centerIn(gtx, px, u.label(unit.Sp(float32(size)*0.48), initial, p.AvatarFgs[i], labelOpts{weight: font.Medium, maxLines: 1}).Layout)
		return dims
	}
	fillCircle(gtx, mid, px/2, p.UserAvatar)
	centerIn(gtx, px, iconW(icPerson, size*0.62, p.UserAvatarIcon))
	return dims
}

// avatarInitial is the letter a person without a picture shows: the first
// of their name, or "" when it doesn't start with a letter (a phone
// number, an emoji).
func avatarInitial(name string) string {
	name = strings.TrimLeft(stripIsolates(plainText(name)), "~ ⁨⁩")
	for _, r := range name {
		if unicode.IsLetter(r) {
			return string(unicode.ToUpper(r))
		}
		break
	}
	return ""
}

// avatarTint picks the color of someone's initial from their ID, so it
// stays the same wherever they show up.
func avatarTint(id, name string, n int) int {
	if id == "" {
		id = name
	}
	h := fnv.New32a()
	h.Write([]byte(id))
	return int(h.Sum32() % uint32(n))
}
