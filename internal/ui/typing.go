package ui

import (
	"image"
	"slices"
	"strconv"

	"gioui.org/op"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Only actual editor changes announce typing. Restoring a draft or opening
// a chat must not tell the other person we're writing to them.
func (u *UI) canReportTyping() bool {
	c := u.selected
	return c != nil && !c.Self && !u.away && !u.ghostMode() &&
		!u.postingStatus() && u.sendBlocked(c) == "" &&
		trimSpace(u.conv.composer.Text()) != "" && u.slashQuery() == nil
}

func (u *UI) reportComposerTyping() {
	if !u.canReportTyping() {
		u.stopOutgoingTyping()
		return
	}
	u.conv.outgoingTyping = u.selected.ID
	u.backend.ReportTyping(u.selected.ID)
}

func (u *UI) stopOutgoingTyping() {
	if u.conv.outgoingTyping != "" {
		u.backend.ReportTyping("")
		u.conv.outgoingTyping = ""
	}
}

// typingText is who is typing in c, for its header and the chat list:
// "typing…" in a one-to-one chat, names in a group.
func typingText(c *model.Chat) string {
	ts := c.Typing
	if len(ts) == 0 {
		return ""
	}
	if !c.IsGroup {
		return "typing…"
	}
	name := func(i int) string { return shortName(ts[i].Name) }
	switch len(ts) {
	case 1:
		return name(0) + " is typing…"
	case 2:
		return name(0) + " and " + name(1) + " are typing…"
	case 3:
		return name(0) + ", " + name(1) + " and " + name(2) + " are typing…"
	}
	return name(0) + ", " + name(1) + " and " + strconv.Itoa(len(ts)-2) + " others are typing…"
}

// typistAnim is someone in the typing bubble's row of avatars, popping in
// when they start typing and out when they stop.
type typistAnim struct {
	model.Typist
	on bool
	v  tween
}

// maxTypists is how many avatars the typing bubble shows at most.
const maxTypists = 3

// syncTypists brings the typing bubble's avatars up to who is typing now.
// Newcomers join the end of the row; snap shows them at once (the bubble
// itself is only now growing in, or the chat just opened).
func (u *UI) syncTypists(ts []model.Typist, snap bool) {
	same := func(a, b model.Typist) bool { return a.ID == b.ID && (a.ID != "" || a.Name == b.Name) }
	if snap {
		u.conv.typists = u.conv.typists[:0]
	}
	for i := range u.conv.typists {
		a := &u.conv.typists[i]
		j := slices.IndexFunc(ts, func(t model.Typist) bool { return same(t, a.Typist) })
		a.on = j >= 0
		if a.on {
			a.Typist = ts[j] // their name may have resolved since
		}
	}
	for _, t := range ts {
		if slices.ContainsFunc(u.conv.typists, func(a typistAnim) bool { return same(a.Typist, t) }) {
			continue
		}
		a := typistAnim{Typist: t, on: true}
		if snap {
			a.v.snap(true)
		}
		u.conv.typists = append(u.conv.typists, a)
	}
}

// layoutTypists draws the avatars of who is typing in a row from x0, the
// first in the left margin where a message's avatar goes. Each pops in or
// out and the ones after it slide over. It returns how far the bubble
// moves right to clear the avatars after the first.
func (u *UI) layoutTypists(gtx C, x0 int) int {
	const size = 29
	sz, ring := gtx.Dp(size), gtx.Dp(2)
	step := float32(sz - ring) // they overlap a little, each ringed in the chat's color
	x := float32(x0)
	shown := 0
	for i := range u.conv.typists {
		a := &u.conv.typists[i]
		if shown == maxTypists {
			break
		}
		shown++
		v := a.v.step(gtx, a.on, durAppear)
		if v == 0 {
			continue
		}
		at := image.Pt(int(x+0.5), 0)
		mid := at.Add(image.Pt(sz/2, sz/2))
		fx := pushFx(gtx, easeOut(v), scaleAt(mid, lerp(0.5, 1, easeOutBack(v))))
		if x > float32(x0) {
			fillCircle(gtx, mid, sz/2+ring, u.pal.ChatBg)
		}
		t := op.Offset(at).Push(gtx.Ops)
		u.avatar(gtx, a.ID, a.Name, false, size)
		t.Pop()
		fx.Pop()
		x += step * easeInOut(v) // the same curve both ways, so leaving slides evenly too
	}
	u.conv.typists = slices.DeleteFunc(u.conv.typists, func(a typistAnim) bool { return !a.on && a.v.v == 0 })
	return max(0, int(x-float32(x0)-step+0.5))
}
