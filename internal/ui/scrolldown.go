package ui

import (
	"image"
	"slices"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Three things float over the open chat's messages, like WhatsApp's:
//   - a round ⌄ button at the bottom right while you're scrolled up from
//     the newest message, which goes back down to it, with the number of
//     unread messages below on it. After clicks on replies' quotes it goes
//     back to those replies first, the oldest one first, and shows a single
//     ⌄ while it does: going to the newest message it's a double one;
//   - in a group, an @ button above it while unread messages mention you (or
//     reply to you), with how many: each click goes to the next;
//   - the day you're looking at, pinned at the top of the chat while you
//     scroll and faded out after. The next day's chip pushes it up.
//
// The day chips of the list are drawn here too, after the list, so the
// one pinned at the top can take the place of the one in the list.

// convFloat is the state of the button and the pinned day.
type convFloat struct {
	chat     string // the chat it belongs to
	down     widget.Clickable
	downAnim tween
	away     bool // scrolled up from the newest message, last frame
	// below is the unread messages under the screen, on the button: the
	// chat's unread ones when it opened, and those that came since.
	below int
	// back are the replies whose quotes were clicked that are below the
	// screen, oldest first whatever the order of the clicks: where the
	// button goes before the newest message.
	back     []backJump
	backAnim tween // the button's icon: ⌄ while there are any
	// mentions are the unread messages for you that weren't on screen
	// yet, oldest first.
	mentions []string
	at       widget.Clickable
	atAnim   tween
	atN      int // how many it said last, while it goes away
	dateAnim tween
	pinned   bool      // the day's chip was pinned at the top last frame
	scrolled time.Time // when the list last scrolled
	pos      layout.Position
	chips    []dayChip // the day chips laid out this frame
}

// backJump is a reply to go back to.
type backJump struct {
	id string
	at time.Time // when it was sent
}

// addBack adds reply m to the replies to go back to.
func (f *convFloat) addBack(m *model.Message) {
	f.back = slices.DeleteFunc(f.back, func(b backJump) bool { return b.id == m.ID })
	i := slices.IndexFunc(f.back, func(b backJump) bool { return !b.at.Before(m.Time) })
	if i < 0 {
		i = len(f.back)
	}
	f.back = slices.Insert(f.back, i, backJump{id: m.ID, at: m.Time})
}

// dayChip is a day chip of the list, recorded while it laid out.
type dayChip struct {
	row  int
	top  int // from the top of its row
	size image.Point
	call op.CallOp
}

const (
	// downAway is how far from the newest message the list must be
	// scrolled for the button to show.
	downAway unit.Dp = 100
	// dateLinger is how long the pinned day stays after scrolling stops.
	dateLinger = 1500 * time.Millisecond
	// dateTop is the pinned day's distance from the top of the chat: where
	// a day chip at the top of the list sits.
	dateTop unit.Dp = 10
)

// layoutDayChip records the day chip of row i, which layoutFloats draws.
// top is the row's top inset.
func (u *UI) layoutDayChip(gtx C, i int, top unit.Dp, txt string) D {
	m := op.Record(gtx.Ops)
	d := u.systemChip(gtx, txt)
	f := &u.conv.float
	f.chips = append(f.chips, dayChip{row: i, top: gtx.Dp(top), size: d.Size, call: m.Stop()})
	return d
}

// newBelow counts a message that just arrived in the open chat while
// you're scrolled up from it.
func (u *UI) newBelow(e model.MessageEvent) {
	f := &u.conv.float
	if e.New && !e.Msg.FromMe && u.selected != nil && u.selected.ID == e.Msg.ChatID && f.away {
		f.below++
		if u.selected.IsGroup && e.Msg.ForMe(u.meID) {
			f.mentions = append(f.mentions, e.Msg.ID)
		}
	}
}

// trackFloats drops the replies to go back to that are on screen again (or
// were scrolled past), and the mentions seen.
func (u *UI) trackFloats(rows []convRow) {
	f := &u.conv.float
	pos := u.conv.list.Position
	first, last := pos.First, min(pos.First+pos.Count, len(rows))
	seen := func(id string) bool {
		for i := first; i < last; i++ {
			if rows[i].has(id) {
				return true
			}
		}
		return false
	}
	if g := &u.conv.glide; !g.pending && !g.active && u.conv.scrollTo == nil {
		f.back = slices.DeleteFunc(f.back, func(b backJump) bool {
			at := slices.IndexFunc(rows, func(r convRow) bool { return r.has(b.id) })
			if at < 0 {
				// Not loaded: still to come when it's below the loaded ones.
				return !u.conv.newerMore || len(u.msgs) > 0 && b.at.Before(u.msgs[len(u.msgs)-1].Time)
			}
			return at < last
		})
		f.mentions = slices.DeleteFunc(f.mentions, seen)
	}
}

// layoutFloats draws the list's day chips, the pinned day and the button
// over the messages, which have just been laid out as rows.
func (u *UI) layoutFloats(gtx C, c *model.Chat, rows []convRow) {
	f := &u.conv.float
	chips := f.chips
	f.chips = f.chips[:0]
	l := &u.conv.list
	pos := l.Position
	sz := gtx.Constraints.Max
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()

	away := u.conv.newerMore || pos.First+pos.Count < len(rows) || -pos.OffsetLast > gtx.Dp(downAway)
	if f.chat != c.ID {
		// Opening a chat isn't scrolling it.
		f.chat, f.pos, f.pinned = c.ID, pos, false
		f.scrolled = time.Time{}
		f.dateAnim.snap(false)
		f.downAnim.snap(away)
	}
	// Following the newest message as it comes isn't scrolling either.
	if (pos.First != f.pos.First || pos.Offset != f.pos.Offset) && (pos.BeforeEnd || f.pos.BeforeEnd) && !gtx.Now.IsZero() {
		f.scrolled = gtx.Now
	}
	f.pos = pos
	f.away = away
	u.trackFloats(rows)
	if !away {
		f.below = 0 // you've seen them
	}

	// Where the visible day chips are, from the top of the list.
	ys := make(map[int]int, 4)
	y := -pos.Offset
	for i := pos.First; i < pos.First+pos.Count && i < len(rows); i++ {
		ys[i] = y
		y += u.conv.heights[i]
	}
	// The day at the top of the list.
	day := -1
	for i := min(pos.First, len(rows)-1); i >= 0; i-- {
		if rows[i].kind == rowDate {
			day = i
			break
		}
	}
	top := gtx.Dp(dateTop)
	pin := day >= 0
	for _, ch := range chips {
		if ch.row == day {
			// Still where it is in the list.
			pin = ys[ch.row]+ch.top < top
		}
	}
	on := pin && !f.scrolled.IsZero() && gtx.Now.Sub(f.scrolled) < dateLinger
	if on && !f.pinned {
		// The chip in the list goes on as the pinned one: no fade.
		f.dateAnim.snap(true)
	}
	f.pinned = pin
	if on {
		gtx.Execute(op.InvalidateCmd{At: f.scrolled.Add(dateLinger)})
	}
	a := f.dateAnim.step(gtx, on, durHoverOut)
	if !pin {
		a = 0
	}

	for _, ch := range chips {
		cy, ok := ys[ch.row]
		if !ok {
			continue
		}
		draw := func() {
			defer op.Offset(image.Pt((sz.X-ch.size.X)/2, cy+ch.top)).Push(gtx.Ops).Pop()
			ch.call.Add(gtx.Ops)
		}
		if ch.row == day {
			withOpacity(gtx, 1-a, draw) // the pinned one takes its place
		} else {
			draw()
		}
	}
	if a > 0 {
		m := op.Record(gtx.Ops)
		d := u.systemChip(gtx, rows[day].date)
		call := m.Stop()
		py := top
		// The next day's chip pushes it up.
		for _, ch := range chips {
			if cy, ok := ys[ch.row]; ok && ch.row > day {
				py = min(py, cy+ch.top-d.Size.Y-gtx.Dp(6))
				break
			}
		}
		t := op.Offset(image.Pt((sz.X-d.Size.X)/2, py)).Push(gtx.Ops)
		withOpacity(gtx, a, func() { call.Add(gtx.Ops) })
		t.Pop()
	}

	u.layoutDownButton(gtx, sz)
	u.layoutMentionButton(gtx, sz)
}

// goDown is the ⌄ button: back to the oldest of the replies whose quotes
// were clicked, or else to the newest message.
func (u *UI) goDown() {
	f := &u.conv.float
	for len(f.back) > 0 {
		id := f.back[0].id
		f.back = f.back[1:]
		if u.jumpTo(id) {
			return
		}
	}
	u.scrollMessages(layout.Position{})
	f.below = 0
}

// nextMention goes to the oldest unread message for you not seen yet.
func (u *UI) nextMention() {
	f := &u.conv.float
	for len(f.mentions) > 0 {
		id := f.mentions[0]
		f.mentions = f.mentions[1:]
		if u.jumpTo(id) {
			return
		}
	}
}

// floatButton draws a round button of the floats at at, v grown in, with
// n over its top left when there are any. Going away (on false) it's
// drawn, but no longer a button.
func (u *UI) floatButton(gtx C, c *widget.Clickable, at image.Point, s int, v float32, on bool, n int, ic layout.Widget) {
	p := u.pal
	e := easeOut(v)
	defer op.Offset(at).Push(gtx.Ops).Pop()
	defer pushFx(gtx, e, scaleAt(image.Pt(s/2, s/2), lerp(0.7, 1, e))).Pop()
	r := s / 2
	rect := image.Rect(0, 0, s, s)
	fillRRect(gtx, rect.Add(image.Pt(0, gtx.Dp(2))).Inset(-gtx.Dp(1)), r+gtx.Dp(1), p.Shadow)
	if on {
		clickable(gtx, c, func(gtx C) D {
			fillCircle(gtx, image.Pt(r, r), r, p.Menu)
			if h := u.hover(gtx, c); h > 0 {
				fillCircle(gtx, image.Pt(r, r), r, faded(p.MenuHover, h))
			}
			return centerIn(gtx, s, ic)
		})
	} else {
		fillCircle(gtx, image.Pt(r, r), r, p.Menu)
		centerIn(gtx, s, ic)
	}
	if n > 0 {
		// Over the button's top left, like WhatsApp's.
		defer op.Offset(image.Pt(-gtx.Dp(6), -gtx.Dp(10))).Push(gtx.Ops).Pop()
		u.badge(gtx, n)
	}
}

// layoutMentionButton draws the @ button above the ⌄ button, or in its
// place while that one is away.
func (u *UI) layoutMentionButton(gtx C, sz image.Point) {
	f := &u.conv.float
	if f.at.Clicked(gtx) {
		u.nextMention()
		gtx.Execute(op.InvalidateCmd{})
	}
	on := len(f.mentions) > 0
	v := f.atAnim.step(gtx, on, durPopIn)
	if v <= 0 {
		return
	}
	if on {
		f.atN = len(f.mentions)
	}
	s := gtx.Dp(42)
	up := int(float32(s+gtx.Dp(16)) * easeOut(f.downAnim.v))
	at := image.Pt(sz.X-s-gtx.Dp(18), sz.Y-s-gtx.Dp(14)-up)
	u.floatButton(gtx, &f.at, at, s, v, on, f.atN,
		u.label(23, "@", u.pal.Icon, labelOpts{weight: font.SemiBold, maxLines: 1}).Layout)
}

// layoutDownButton draws the ⌄ button at the bottom right of a list of
// size sz while it's scrolled up, with the unread count over it. One ⌄
// goes back to a reply, two to the newest message.
func (u *UI) layoutDownButton(gtx C, sz image.Point) {
	f := &u.conv.float
	if f.down.Clicked(gtx) {
		u.goDown()
	}
	v := f.downAnim.step(gtx, f.away, durPopIn)
	if v <= 0 {
		return
	}
	s := gtx.Dp(42)
	at := image.Pt(sz.X-s-gtx.Dp(18), sz.Y-s-gtx.Dp(14))
	u.floatButton(gtx, &f.down, at, s, v, f.away, f.below, func(gtx C) D {
		b := f.backAnim.step(gtx, len(f.back) > 0, durHoverOut)
		withOpacity(gtx, b, func() { drawIcon(gtx, icChevron, 28, u.pal.Icon) })
		withOpacity(gtx, 1-b, func() { drawIcon(gtx, icChevronTwice, 28, u.pal.Icon) })
		return D{Size: image.Pt(gtx.Dp(28), gtx.Dp(28))}
	})
}
