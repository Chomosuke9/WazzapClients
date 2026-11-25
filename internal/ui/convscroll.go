package ui

import (
	"image"
	"math"

	"gioui.org/layout"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// The open chat's scrollbar stands for the loaded messages (paging.go), so
// a pixel of it stays a few lines however long the chat is. No page loads
// while its thumb is dragged: one would put the thumb's place on other
// messages, and holding it near the top read page after page. The page
// loads once the thumb is let go, and the thumb glides to its new place.
//
// A place among the rows is counted in rows: 12.5 is the middle of the
// 13th. The thumb is t (0 to 1) down its track where the place t down the
// screen is t of all rows, which puts the rows' ends at the track's and is
// exact both ways: scrolling moves the thumb without jumps whatever the
// rows' heights, and the list follows a dragged thumb without estimating
// the heights of rows it hasn't laid out.

// convScroll is the state of the open chat's scrollbar.
type convScroll struct {
	chat   string
	typing int // the typing row, which has no place; -1 for none
	total  int // rows, less the typing one
	place  seekPlace
	// avg is the px a row takes, for the thumb's size: a mean of what was
	// on screen lately, so the thumb doesn't change with every row.
	avg      float32
	avgFirst int // the top row avg was last updated for
}

// seekPlace is a place to put on the screen, set while the list lays out
// its row, once its height is known.
type seekPlace struct {
	on   bool
	row  int
	frac float64 // how far into the row the place is, 0 to 1
	y    float64 // where it goes, px from the list's top
}

// measure notes the rows the bar stands for.
func (s *convScroll) measure(rows []convRow) {
	s.typing, s.total = -1, len(rows)
	for i := len(rows) - 1; i >= 0 && rows[i].msg == nil; i-- {
		if rows[i].kind == rowTyping {
			s.typing = i
			s.total--
		}
	}
}

// span is where row i starts and how long it is, in rows.
func (s *convScroll) span(i int) (start, n float64) {
	switch {
	case i == s.typing:
		return float64(i), 0
	case s.typing >= 0 && i > s.typing:
		return float64(i - 1), 1
	}
	return float64(i), 1
}

// dragMessages handles the scrollbar's pointer before the rows are laid
// out. It reports whether the list is to go to the thumb's place (seekTo).
func (u *UI) dragMessages(gtx C, c *model.Chat) bool {
	s := &u.conv.scroll
	b := u.bar(&u.conv.list)
	if s.chat != c.ID {
		s.chat, s.avg = c.ID, 0
		b.dragging, b.from = false, barGlide{}
	}
	s.place.on = false
	if !b.update(gtx, &u.conv.list) {
		return false
	}
	u.conv.glide = glide{}
	u.conv.scrollTo, u.conv.scrollAbove = nil, 0
	return true
}

// seekTo puts the list at the thumb's place. rows are the ones about to be
// laid out, in a list of height view.
func (u *UI) seekTo(rows []convRow, view int) {
	s := &u.conv.scroll
	l := &u.conv.list
	t := float64(u.bar(l).t)
	s.measure(rows)
	switch {
	case u.conv.scrollTo != nil || s.total <= 0:
		return
	case t >= 1:
		l.Position = layout.Position{First: len(rows)}
		return
	case t <= 0:
		l.Position = layout.Position{BeforeEnd: true}
		return
	}
	p := t * float64(s.total)
	i := int(p)
	if s.typing >= 0 && i >= s.typing {
		i++
	}
	start, _ := s.span(i)
	s.place = seekPlace{on: true, row: i, frac: p - start, y: t * float64(view)}
	l.Position = layout.Position{First: i, BeforeEnd: true}
}

// laidOut finishes a seek as row i, h px tall, is laid out. The list lays
// its first row out first and reads the offset after.
func (s *convScroll) laidOut(l *layout.List, i, h int) {
	if s.place.on && s.place.row == i {
		s.place.on = false
		l.Position.Offset = int(math.Round(s.place.frac*float64(h) - s.place.y))
	}
}

// messageList lays out the conversation's rows with its scrollbar.
func (u *UI) messageList(gtx C, rows []convRow, el layout.ListElement) D {
	l := &u.conv.list
	return u.wheelList(gtx, &l.List, func(gtx C) D {
		dims := l.List.Layout(gtx, len(rows), el)
		u.layoutConvBar(gtx, rows, dims.Size)
		return dims
	})
}

// layoutConvBar draws the scrollbar over the list just laid out.
func (u *UI) layoutConvBar(gtx C, rows []convRow, size image.Point) {
	s := &u.conv.scroll
	l := &u.conv.list
	b := u.bar(l)
	pos := l.Position
	view := size.Y
	atStart := pos.First == 0 && pos.Offset <= 0
	atEnd := pos.First+pos.Count >= len(rows) && pos.OffsetLast >= 0
	if len(rows) == 0 || view <= 0 || atStart && atEnd {
		b.n, b.dragging = 0, false
		return
	}
	s.measure(rows)
	t, shown := s.thumbAt(pos, u.conv.heights, view)
	if shown > 0 {
		switch avg := float32(float64(view) / shown); {
		case s.avg <= 0:
			s.avg, s.avgFirst = avg, pos.First
		case s.avgFirst != pos.First && !b.dragging:
			s.avg += (avg - s.avg) / 10
			s.avgFirst = pos.First
		}
	}
	switch {
	case b.dragging:
		t = b.t
	case atStart:
		t = 0
	case atEnd:
		t = 1
	}
	content := max(s.avg*float32(u.barRows()), float32(view+1))
	b.draw(gtx, u.pal, size, len(rows), content, t)
}

// barRows is how many rows the thumb is sized for. It shrinks as pages
// load, and grows when all of a short chat has. With more messages stored,
// a third of what could still load counts too, or the thumb of a chat just
// opened would be big.
func (u *UI) barRows() int {
	n := u.conv.scroll.total
	if u.conv.olderMore || u.conv.newerMore {
		n += max(0, perf.loadedMsgs-n) / 3
	}
	return n
}

// thumbAt is where the thumb goes, 0 to 1, with the list at pos, and how
// many rows are on screen.
func (s *convScroll) thumbAt(pos layout.Position, heights map[int]int, view int) (t float32, shown float64) {
	perPx := float64(s.total) / float64(view)
	found := false
	t = 1
	y := float64(-pos.Offset)
	for i := pos.First; i < pos.First+pos.Count; i++ {
		h := float64(heights[i])
		p, n := s.span(i)
		top := y
		y += h
		y0, y1 := max(top, 0), min(y, float64(view))
		if y1 <= y0 {
			continue
		}
		p0, p1 := p+n*(y0-top)/h, p+n*(y1-top)/h
		shown += p1 - p0
		// How far ahead of the screen's share of the rows the place is:
		// it starts at 0 or more and ends at 0 or less.
		g0, g1 := p0-y0*perPx, p1-y1*perPx
		if !found && g1 <= 0 {
			found = true
			at := y0
			if g0 > g1 && g0 > 0 {
				at += (y1 - y0) * g0 / (g0 - g1)
			}
			t = float32(at / float64(view))
		}
	}
	return min(1, max(0, t)), shown
}
