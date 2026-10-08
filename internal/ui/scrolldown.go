package ui

import (
	"image"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Two things float over the open chat's messages, like WhatsApp's:
//   - a round ⌄ button at the bottom right while you're scrolled up from
//     the newest message, which goes back down to it, with the number of
//     unread messages below on it;
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
	below    int
	dateAnim tween
	pinned   bool      // the day's chip was pinned at the top last frame
	scrolled time.Time // when the list last scrolled
	pos      layout.Position
	chips    []dayChip // the day chips laid out this frame
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
}

// layoutDownButton draws the ⌄ button at the bottom right of a list of
// size sz while it's scrolled up, with the unread count over it.
func (u *UI) layoutDownButton(gtx C, sz image.Point) {
	f := &u.conv.float
	p := u.pal
	if f.down.Clicked(gtx) {
		u.scrollMessages(layout.Position{})
		f.below = 0
	}
	v := f.downAnim.step(gtx, f.away, durPopIn)
	if v <= 0 {
		return
	}
	e := easeOut(v)
	s := gtx.Dp(42)
	at := image.Pt(sz.X-s-gtx.Dp(18), sz.Y-s-gtx.Dp(14))
	defer op.Offset(at).Push(gtx.Ops).Pop()
	defer pushFx(gtx, e, scaleAt(image.Pt(s/2, s/2), lerp(0.7, 1, e))).Pop()
	r := s / 2
	rect := image.Rect(0, 0, s, s)
	fillRRect(gtx, rect.Add(image.Pt(0, gtx.Dp(2))).Inset(-gtx.Dp(1)), r+gtx.Dp(1), p.Shadow)
	if f.away {
		clickable(gtx, &f.down, func(gtx C) D {
			fillCircle(gtx, image.Pt(r, r), r, p.Menu)
			if h := u.hover(gtx, &f.down); h > 0 {
				fillCircle(gtx, image.Pt(r, r), r, faded(p.MenuHover, h))
			}
			return centerIn(gtx, s, iconW(icChevron, 28, p.Icon))
		})
	} else {
		// Going away: drawn, but no longer a button.
		fillCircle(gtx, image.Pt(r, r), r, p.Menu)
		centerIn(gtx, s, iconW(icChevron, 28, p.Icon))
	}
	if f.below > 0 {
		// Over the button's top left, like WhatsApp's.
		defer op.Offset(image.Pt(-gtx.Dp(6), -gtx.Dp(10))).Push(gtx.Ops).Pop()
		u.badge(gtx, f.below)
	}
}
