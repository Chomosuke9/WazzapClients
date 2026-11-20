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

// Sticker reactions: the stickers people reacted to a message with (up to
// model.StickerReactsMax each) sit around its bubble, as on WhatsApp's
// phones. The newest is first, at the bubble's bottom edge: from the
// bubble's edge (the left one of others' messages, the right one of
// yours), or from the pill of emoji reactions when there is one. A few
// make a row along the bottom, which may reach past the bubble's other
// edge; more go on around the bubble: up its other side and back along
// its top (stickerRing), squeezed closer together, the oldest left out
// when they can't squeeze more. Each leans a little, every other one the
// other way, and has rounded corners, like a sticker stuck on by hand. Clicking one lists who
// reacted (reactions.go), where yours are taken back one by one; the
// reaction picker's Sticker tab gives one.

const (
	// stickerReactDp is a sticker reaction's size, and
	// stickerReactOverlapDp how far it reaches over the bubble's edges.
	// Read off a phone's screen recordings against the text beside them:
	// a sticker is about two lines of message text tall.
	stickerReactDp        = 36
	stickerReactOverlapDp = 7
	// stickerReactGapDp is the space between stickers in a row that
	// isn't squeezed, stickerReactStartDp where the row starts, past the
	// bubble's edge, and stickerReactPillGapDp how far after the emojis'
	// pill it starts when there is one.
	stickerReactGapDp     = 2
	stickerReactStartDp   = -6
	stickerReactPillGapDp = 4
	// stickerReactMax is how many of a message's sticker reactions show
	// at most: the newest. Fewer show when they don't fit around the
	// bubble even squeezed.
	stickerReactMax = 60
	// stickerSqueezeMin is how close stickers may come along the ring, as
	// a share of their size. WhatsApp's phones squeeze them about this
	// far and leave out the oldest that still don't fit (12 of 18 around
	// a small bubble).
	stickerSqueezeMin = 0.8
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
	count int         // how many stickers its message showed
	start int         // where the row started (it moves with the pill)
	pos   image.Point // that place, in the bubble
	tilt  float32
	from  image.Point // where it moves from, less pos, since moved
	fromT float32     // the tilt it had there
	moved time.Time
	born  time.Time // when it faded in; zero if it was there already
}

const (
	// A phone's screen recording: the others reach their new places in
	// about 200 ms, easing out, and a new sticker fades in within two
	// frames, at full size. One that no longer fits goes at once, and a
	// pill coming or going moves them all at once.
	stickerMoveDur = 220 * time.Millisecond
	stickerFadeDur = 80 * time.Millisecond
	// stickerTiltRad is how far a sticker reaction leans: clockwise in
	// the first, third... places, the other way in the others.
	stickerTiltRad = 4 * math.Pi / 180
)

// stickerReactions draws the sticker reactions to ms (a message, or an
// album's pictures) around their bubble, bubble big at the origin, after
// reactionPill drew the emojis, and returns dims grown by the stickers
// under the bubble.
//
// A sticker that just came fades in, in front of the others, while those
// glide to their new places.
func (u *UI) stickerReactions(gtx C, ms []*model.Message, bubble image.Point, dims D, out bool) D {
	all := shownStickers(ms)
	if len(all) == 0 || !u.takesStickers(ms[0]) {
		return dims
	}
	sz, gap := gtx.Dp(stickerReactDp), gtx.Dp(stickerReactGapDp)
	// Places are worked out for someone else's message and mirrored for
	// yours: around the bubble from its edge, or from the emojis' pill,
	// the newest first. at[i] is the place of all[i], and slot[i] which
	// one of the ring it is.
	start := gtx.Dp(stickerReactStartDp)
	if pill := u.conv.pill; !pill.Empty() {
		end := pill.Max.X
		if out {
			end = bubble.X - pill.Min.X
		}
		start = end + gtx.Dp(stickerReactPillGapDp)
	}
	ring := stickerRing(len(all), sz, gap, gtx.Dp(stickerReactOverlapDp), start, bubble)
	all = all[len(all)-len(ring):]
	at := make([]image.Point, len(all))
	slot := make([]int, len(all))
	for i := range all {
		n := len(all) - 1 - i
		slot[i] = n
		at[i] = ring[n]
	}
	if u.conv.stickerSpots == nil || len(u.conv.stickerSpots) > 2048 {
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
			sp = &stickerSpot{slot: slot[i], count: len(all), start: start}
			if age := u.now().Sub(r.Time); animate && age >= 0 && age < 10*time.Second {
				sp.born = now // it came just now
			}
			u.conv.stickerSpots[r.ID] = sp
		case sp.slot != slot[i] || sp.count != len(all) && sp.start == start:
			// Another sticker came or went: move from where it is now.
			cur, curT := sp.at(now)
			sp.slot, sp.from, sp.fromT, sp.moved = slot[i], cur.Sub(pos), curT, now
		}
		sp.count, sp.start = len(all), start
		sp.pos, sp.tilt = pos, tilt
		alpha := float32(1)
		if animate {
			pos, tilt = sp.at(now)
			if !sp.born.IsZero() {
				if v := float32(now.Sub(sp.born)) / float32(stickerFadeDur); v < 1 {
					alpha = max(v, 0)
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
		fx := pushFx(gtx, alpha, rotateAt(mid, tilt))
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
		dims.Size.Y = max(dims.Size.Y, at[i].Y+sz)
	}
	return dims
}

// stickerRing returns the places of n sticker reactions of size sz around
// a bubble of size bubble at the origin (someone else's message: mirror
// them for yours), the newest first: from start along the bottom edge,
// overlapping it, then up the bubble's right side and back left along its
// top, to above start. The ring is as wide as the bubble (from start, so
// narrower after the emojis' pill), and a single column when that's less
// than a sticker. A row that fits along the bottom keeps gap between its
// stickers; more are spread evenly along each side of the ring, a sticker
// in each corner, the rows along the bottom and top squeezed together
// until they fit while the side keeps its spacing, as on WhatsApp's
// phones. It returns fewer places than n when they don't fit even
// squeezed: the newest that do.
func stickerRing(n, sz, gap, overlap, start int, bubble image.Point) []image.Point {
	if n <= 0 {
		return nil
	}
	d := sz + gap
	bottom, top := bubble.Y-overlap, overlap-sz
	right := bubble.X - overlap
	if right-start < d*3/4 {
		right = start // no room for a row
	}
	// The corners: from start, right along the bottom, up, left along the
	// top to above start.
	corners := [...]image.Point{{start, bottom}, {right, bottom}, {right, top}, {start, top}}
	// The closest stickers come, apart.
	closest := float32(sz) * stickerSqueezeMin
	steps := func(i, d int) int {
		if i == 1 {
			d = sz + gap // the side isn't squeezed
		}
		l := corners[i+1].Sub(corners[i])
		length := float32(max(l.X, -l.X, l.Y, -l.Y))
		k := int(length/float32(d) + 0.25)
		if i != 1 {
			k = min(k, int(length/closest))
		}
		if i == 1 || l != (image.Point{}) {
			k = max(k, 1)
		}
		return k
	}
	fits := func(d int) int { return 1 + steps(0, d) + steps(1, d) + steps(2, d) }
	if n <= steps(0, d)+1 {
		// One row along the bottom.
		at := make([]image.Point, n)
		for i := range at {
			at[i] = image.Pt(start+i*d, bottom)
		}
		return at
	}
	for dmin := max(int(closest), 1); d > dmin && fits(d) < n; d-- {
	}
	at := []image.Point{corners[0]}
	for i := 0; i < 3 && len(at) < n; i++ {
		k := steps(i, d)
		a, b := corners[i], corners[i+1]
		for j := 1; j <= k && len(at) < n; j++ {
			at = append(at, a.Add(b.Sub(a).Mul(j).Div(k)))
		}
	}
	return at
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
