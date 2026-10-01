package ui

import (
	"image"
	"strings"
	"time"
	"unicode"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/op/clip"

	"github.com/chomosuke9/wazzapclients/internal/ui/styledtext"
)

// textSelection is the selected part of one message's text, as in a
// browser: a drag selects, a double click a word and a triple click the
// whole text. Only the text itself is selectable, not the sender, quote,
// time or footer. Positions count the runes of the text as displayed
// (formatting markers removed), with one rune between blocks.
type textSelection struct {
	id         string // the message, or "" for none
	anchor, at int    // where the selection started and where it ends now
	dragging   bool

	clicks    int // presses in a row, for double and triple clicks
	lastClick time.Duration
	lastPos   image.Point
	pressed   time.Duration // the last press on selectable text, see trackMouse

	// The displayed text of the message last laid out with carets.
	src   string
	plain []rune

	carets []styledtext.Caret // the message's, for this frame
	tmp    []styledtext.Caret // one block's
	evs    []pointer.Event
	keyTag struct{}
}

func (ts *textSelection) clear() {
	ts.id, ts.anchor, ts.at, ts.dragging = "", 0, 0, false
}

// span returns the selection as start and end, or ok false when nothing
// of message id is selected.
func (ts *textSelection) span(id string) (a, b int, ok bool) {
	if ts.id == "" || ts.id != id {
		return 0, 0, false
	}
	a, b = min(ts.anchor, ts.at), max(ts.anchor, ts.at)
	a, b = min(a, len(ts.plain)), min(b, len(ts.plain))
	return a, b, a < b
}

// selected returns the selected text of message id, ready to copy.
func (ts *textSelection) selected(id string) string {
	a, b, ok := ts.span(id)
	if !ok {
		return ""
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ':
			return ' '
		case ' ': // pads inline code
			return -1
		}
		return r
	}, string(ts.plain[a:b]))
}

// setPlain records the displayed text of blocks, parsed from src.
func (ts *textSelection) setPlain(src string, blocks []richBlock) {
	if ts.plain != nil && ts.src == src {
		return
	}
	ts.src, ts.plain = src, ts.plain[:0]
	for i, b := range blocks {
		if i > 0 {
			ts.plain = append(ts.plain, '\n')
		}
		for _, s := range b.spans {
			ts.plain = append(ts.plain, []rune(s.Content)...)
		}
	}
}

// pointerEvents reads the pointer events of a message's text.
func (ts *textSelection) pointerEvents(gtx C, tag event.Tag) []pointer.Event {
	ts.evs = ts.evs[:0]
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok {
			ts.evs = append(ts.evs, e)
		}
	}
	return ts.evs
}

// handle applies pointer events on message id's text, laid out with
// ts.carets.
func (ts *textSelection) handle(gtx C, id string, evs []pointer.Event) {
	for _, e := range evs {
		pos := e.Position.Round()
		switch e.Kind {
		case pointer.Press:
			if !e.Buttons.Contain(pointer.ButtonPrimary) {
				continue
			}
			ts.pressed = e.Time
			d := pos.Sub(ts.lastPos)
			if ts.id == id && e.Time-ts.lastClick < 400*time.Millisecond && d.X*d.X+d.Y*d.Y < 25 {
				ts.clicks++
			} else {
				ts.clicks = 1
			}
			ts.lastClick, ts.lastPos = e.Time, pos
			i := hitCaret(ts.carets, pos, false)
			switch {
			case ts.clicks == 1 && e.Modifiers.Contain(key.ModShift) && ts.id == id:
				ts.at, ts.dragging = i, true
			case ts.clicks == 1:
				ts.id, ts.anchor, ts.at, ts.dragging = id, i, i, true
			case ts.clicks == 2:
				ts.anchor, ts.at = wordAt(ts.plain, hitCaret(ts.carets, pos, true))
				ts.dragging = false
			default:
				ts.anchor, ts.at, ts.dragging = 0, len(ts.plain), false
			}
			gtx.Execute(key.FocusCmd{Tag: &ts.keyTag})
		case pointer.Drag:
			if ts.dragging && ts.id == id {
				ts.at = hitCaret(ts.carets, pos, false)
			}
		case pointer.Release, pointer.Cancel:
			ts.dragging = false
		}
	}
}

// selectionKeys handles Ctrl+C and Ctrl+A while the selection has the focus.
func (u *UI) selectionKeys(gtx C, id string) {
	ts := &u.textSel
	for {
		ev, ok := gtx.Event(key.FocusFilter{Target: &ts.keyTag},
			key.Filter{Focus: &ts.keyTag, Name: "C", Required: key.ModShortcut},
			key.Filter{Focus: &ts.keyTag, Name: "A", Required: key.ModShortcut})
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		switch e.Name {
		case "C":
			if s := ts.selected(id); s != "" {
				u.pendingCopy = s
			}
		case "A":
			ts.anchor, ts.at = 0, len(ts.plain)
		}
	}
}

// hitCaret returns the rune position of the caret nearest to p: on the
// line under p (or the nearest line), the closest one. With under, it is
// the last caret left of p instead, which starts the character under p.
func hitCaret(cs []styledtext.Caret, p image.Point, under bool) int {
	top, bestDy := 0, -1
	for _, c := range cs {
		dy := 0
		switch {
		case p.Y < c.Top:
			dy = c.Top - p.Y
		case p.Y >= c.Bottom:
			dy = p.Y - c.Bottom + 1
		}
		if bestDy < 0 || dy < bestDy {
			top, bestDy = c.Top, dy
		}
	}
	best, bestDx := 0, -1
	for i, c := range cs {
		if c.Top != top {
			continue
		}
		if under {
			// The line's last caret starts no character on it.
			last := i+1 == len(cs) || cs[i+1].Top != top
			if bestDx < 0 || (c.X <= p.X && !last) {
				best, bestDx = c.Rune, 0
			}
			continue
		}
		if dx := max(p.X-c.X, c.X-p.X); bestDx < 0 || dx < bestDx {
			best, bestDx = c.Rune, dx
		}
	}
	return best
}

// wordAt returns the word, or run of other characters, around rune i.
func wordAt(s []rune, i int) (int, int) {
	if len(s) == 0 {
		return 0, 0
	}
	i = min(max(i, 0), len(s)-1)
	if i > 0 && (i == len(s) || !isWordRune(s[i])) && isWordRune(s[i-1]) {
		i--
	}
	class := func(r rune) int {
		switch {
		case isWordRune(r) || r == '_' || r == '\'':
			return 0
		case r == '\n':
			return 1
		case unicode.IsSpace(r):
			return 2
		}
		return 3
	}
	c := class(s[i])
	if c == 1 {
		return i, i + 1
	}
	a, b := i, i+1
	for a > 0 && class(s[a-1]) == c {
		a--
	}
	for b < len(s) && class(s[b]) == c {
		b++
	}
	return a, b
}

// paintSelection highlights runes a to b of text laid out with carets cs.
func (u *UI) paintSelection(gtx C, cs []styledtext.Caret, a, b int) {
	for i := 0; i < len(cs); {
		top := cs[i].Top
		x0, x1, found := 0, 0, false
		j := i
		for ; j < len(cs) && cs[j].Top == top; j++ {
			c := cs[j]
			if c.Rune >= a && c.Rune <= b {
				if !found {
					x0, found = c.X, true
				}
				x1 = c.X
			}
		}
		if found && x1 > x0 {
			fillRect(gtx, image.Rect(x0, top, x1, cs[i].Bottom), u.pal.Selection)
		}
		i = j
	}
}

// selectableArea registers the input of a message's text, of size sz.
func (u *UI) selectableArea(gtx C, tag event.Tag, id string, sz image.Point) {
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	pointer.CursorText.Add(gtx.Ops)
	event.Op(gtx.Ops, tag)
	if u.textSel.id == id {
		event.Op(gtx.Ops, &u.textSel.keyTag)
	}
}
