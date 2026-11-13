package ui

import (
	"image"
	"math"
	"slices"
	"time"

	"gioui.org/f32"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Sticker reactions: the stickers people reacted to a message with sit
// in a row along the bottom edge of its bubble, as on WhatsApp's phones:
// from the bubble's edge (the left one of others' messages, the right one
// of yours), or from the pill of emoji reactions when there is one, the
// newest first. The row may reach past the bubble's other edge. Each
// leans a little, the middle one the other way, and has rounded corners,
// like a sticker stuck on by hand. Clicking one lists who reacted
// (reactions.go), where yours is taken back; the reaction picker's
// Sticker tab gives one.

const (
	// stickerReactDp is a sticker reaction's size, and
	// stickerReactOverlapDp how far it reaches over the bubble's bottom.
	stickerReactDp        = 52
	stickerReactOverlapDp = 10
	// stickerReactMax is how many of a message's sticker reactions show:
	// the newest.
	stickerReactMax = 3
)

// stickerTab is the "who reacted" list's tab of sticker reactions, and
// what their rows hold as emoji.
const stickerTab = "\x00sticker"

// takesStickers reports whether a message shows sticker reactions and
// takes them: WhatsApp shows none in a channel, in a community's
// announcements or on a sticker.
func (u *UI) takesStickers(m *model.Message) bool {
	return m.Media != model.MediaSticker && !isChannelID(m.ChatID) && u.announcementsOf(u.chatByID(m.ChatID)) == nil
}

// shownSticker is a sticker reaction and the message it reacts to.
type shownSticker struct {
	model.StickerReaction
	msg *model.Message
}

// shownStickers are the sticker reactions to ms that show: the newest
// stickerReactMax, oldest first.
func shownStickers(ms []*model.Message) []shownSticker {
	var all []shownSticker
	for _, m := range ms {
		for _, r := range m.Stickers {
			all = append(all, shownSticker{r, m})
		}
	}
	if len(ms) > 1 {
		slices.SortStableFunc(all, func(a, b shownSticker) int { return a.Time.Compare(b.Time) })
	}
	if len(all) > stickerReactMax {
		all = all[len(all)-stickerReactMax:]
	}
	return all
}

// stickerSpot is where a sticker reaction was last drawn, to move it
// from there when another sticker takes its place.
type stickerSpot struct {
	slot  int         // the place it has, among its message's stickers
	pos   image.Point // that place, in the bubble
	tilt  float32
	from  image.Point // where it moves from, less pos, since moved
	fromT float32     // the tilt it had there
	moved time.Time
	born  time.Time // when it popped in; zero if it was there already
}

const (
	stickerMoveDur = 260 * time.Millisecond
	stickerPopDur  = 320 * time.Millisecond
	// stickerTiltRad is how far a sticker reaction leans: clockwise in
	// the first and third places, the other way in the second.
	stickerTiltRad = 4 * math.Pi / 180
)

// stickerReactions draws the sticker reactions to ms (a message, or an
// album's pictures) around their bubble, bubble big at the origin, after
// reactionPill drew the emojis, and returns dims grown by the stickers
// under the bubble.
//
// A sticker that just came pops in, in front of the others, while those
// glide to their new places.
func (u *UI) stickerReactions(gtx C, ms []*model.Message, bubble image.Point, dims D, out bool) D {
	all := shownStickers(ms)
	if len(all) == 0 || !u.takesStickers(ms[0]) {
		return dims
	}
	sz, gap := gtx.Dp(stickerReactDp), gtx.Dp(1)
	bottom := bubble.Y - gtx.Dp(stickerReactOverlapDp)
	// Places are worked out for someone else's message and mirrored for
	// yours: a row from the bubble's edge, or from the emojis' pill, the
	// newest first. at[i] is the place of all[i], and slot[i] which one of
	// the row it is.
	start, first := -gtx.Dp(4), 0
	if pill := u.conv.pill; !pill.Empty() {
		end := pill.Max.X
		if out {
			end = bubble.X - pill.Min.X
		}
		start, first = end+gtx.Dp(12), 10 // moving over for the pill is a move too
	}
	at := make([]image.Point, len(all))
	slot := make([]int, len(all))
	for i := range all {
		n := len(all) - 1 - i
		slot[i] = first + n
		at[i] = image.Pt(start+n*(sz+gap), bottom)
	}
	if u.conv.stickerSpots == nil || len(u.conv.stickerSpots) > 512 {
		u.conv.stickerSpots = make(map[string]*stickerSpot)
	}
	now := gtx.Now
	animate := perf.animations && !now.IsZero()
	// The oldest first, so the newest is in front.
	for i, r := range all {
		pos := at[i]
		tilt := float32(stickerTiltRad)
		if slot[i]%2 == 1 {
			tilt = -tilt
		}
		if out {
			pos.X = bubble.X - pos.X - sz
		}
		sp := u.conv.stickerSpots[r.ID]
		switch {
		case sp == nil:
			sp = &stickerSpot{slot: slot[i]}
			if age := u.now().Sub(r.Time); animate && age >= 0 && age < 10*time.Second {
				sp.born = now // it came just now
			}
			u.conv.stickerSpots[r.ID] = sp
		case sp.slot != slot[i]:
			// Another sticker came or went: move from where it is now.
			cur, curT := sp.at(now)
			sp.slot, sp.from, sp.fromT, sp.moved = slot[i], cur.Sub(pos), curT, now
		}
		sp.pos, sp.tilt = pos, tilt
		scale := float32(1)
		if animate {
			pos, tilt = sp.at(now)
			if !sp.born.IsZero() {
				if v := float32(now.Sub(sp.born)) / float32(stickerPopDur); v < 1 {
					scale = lerp(0.3, 1, easeOutBack(max(v, 0)))
				} else {
					sp.born = time.Time{}
				}
			}
			if !sp.born.IsZero() || !sp.moved.IsZero() {
				gtx.Execute(op.InvalidateCmd{})
			}
		} else {
			sp.moved, sp.born = time.Time{}, time.Time{}
		}
		c := u.btn("stickerreact:" + r.msg.ChatID + "/" + r.ID)
		if c.Clicked(gtx) {
			u.openReactions(ms)
			u.reacts.tab = stickerTab
		}
		t := op.Offset(pos).Push(gtx.Ops)
		// Stuck on by hand: a little askew, its corners rounded.
		mid := f32.Pt(float32(sz)/2, float32(sz)/2)
		tr := rotateAt(mid, tilt)
		if scale != 1 {
			tr = tr.Scale(mid, f32.Pt(scale, scale))
		}
		fx := pushFx(gtx, 1, tr)
		cl := roundShape(sz, sz, sz/10).Push(gtx.Ops)
		u.stickerPicture(gtx, r.Message(r.msg.ChatID), sz)
		cl.Pop()
		fx.Pop()
		// The button goes over the picture: an animated sticker's own
		// would hide under it anyway.
		cgtx := gtx
		cgtx.Constraints = layout.Exact(image.Pt(sz, sz))
		clickable(cgtx, c, func(gtx C) D { return D{Size: image.Pt(sz, sz)} })
		t.Pop()
		dims.Size.Y = max(dims.Size.Y, bottom+sz)
	}
	return dims
}

// at returns where the sticker is at now, and its tilt: on the way to
// its place, or there.
func (s *stickerSpot) at(now time.Time) (image.Point, float32) {
	if s.moved.IsZero() {
		return s.pos, s.tilt
	}
	v := float32(now.Sub(s.moved)) / float32(stickerMoveDur)
	if v >= 1 {
		s.moved = time.Time{}
		return s.pos, s.tilt
	}
	e := 1 - easeOut(max(v, 0))
	return s.pos.Add(image.Pt(int(float32(s.from.X)*e), int(float32(s.from.Y)*e))), lerp(s.tilt, s.fromT, e)
}

// stickerReactors are the rows of the "who reacted" list for the sticker
// reactions to m.
func stickerReactors(m *model.Message) []reactorRow {
	var rows []reactorRow
	for _, r := range m.Stickers {
		rows = append(rows, reactorRow{
			Reactor: model.Reactor{ID: r.SenderID, Name: r.Sender, Me: r.Me, Emoji: stickerTab, Time: r.Time},
			msg:     m, sticker: r.Message(m.ChatID),
		})
	}
	return rows
}
