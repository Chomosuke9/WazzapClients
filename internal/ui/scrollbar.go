package ui

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/widget"
)

// scrollbar is the state of a list's overlay scrollbar.
//
// material.List turns a drag of its thumb into "scroll by so many items",
// measured against a length that it re-estimates every frame from the
// visible rows only. With rows of different heights (messages, pictures)
// the thumb drifts away from the pointer. This one maps the thumb's
// position straight to a scroll position, and while dragging the thumb
// stays exactly under the pointer.
type scrollbar struct {
	hover, dragging bool
	grab            int     // pointer offset into the thumb when the drag began
	t               float32 // thumb position while dragging, 0 to 1
	est             float32 // estimated row height, frozen while dragging

	// Last frame's geometry, for input handled before this frame's layout.
	n, view, track, thumb, top int
}

// scrollList lays out a vertical list with WhatsApp's thin overlay scrollbar.
func (u *UI) scrollList(gtx C, l *widget.List, n int, el layout.ListElement) D {
	if u.bars == nil {
		u.bars = make(map[*widget.List]*scrollbar)
	}
	b := u.bars[l]
	if b == nil {
		b = &scrollbar{}
		u.bars[l] = b
	}
	b.update(gtx, l)

	dims := l.List.Layout(gtx, n, el)

	pos := l.Position
	view := dims.Size.Y
	atStart := pos.First == 0 && pos.Offset <= 0
	atEnd := pos.First+pos.Count >= n && pos.OffsetLast >= 0
	if n == 0 || view <= 0 || atStart && atEnd {
		b.n, b.dragging = 0, false
		return dims
	}
	if !b.dragging || b.est <= 0 {
		b.est = float32(pos.Length) / float32(n)
	}
	content := max(float32(n)*b.est, float32(view+1))
	pad := gtx.Dp(2)
	track := view - 2*pad
	thumb := min(track, max(gtx.Dp(32), int(float32(track)*float32(view)/content)))
	t := b.t
	if !b.dragging {
		switch {
		case atStart:
			t = 0
		case atEnd:
			t = 1
		default:
			s := float32(pos.First)*b.est + float32(pos.Offset)
			t = min(1, max(0, s/(content-float32(view))))
		}
	}
	top := pad + int(t*float32(track-thumb)+0.5)
	b.n, b.view, b.track, b.thumb, b.top = n, view, track, thumb, top

	// The hit area is wider than the thumb.
	w := dims.Size.X
	hit := image.Rect(w-gtx.Dp(10), 0, w, view)
	area := clip.Rect(hit).Push(gtx.Ops)
	event.Op(gtx.Ops, b)
	area.Pop()

	col := u.pal.TextSecondary
	col.A = 0x50
	if b.hover || b.dragging {
		col.A = 0x90
	}
	bw := gtx.Dp(5)
	x := w - pad - bw
	fillRRect(gtx, image.Rect(x, top, x+bw, top+thumb), gtx.Dp(3), col)
	return dims
}

// update handles the pointer: pressing the thumb grabs it; pressing the
// track grabs the thumb by its middle there. Either way it then follows
// the pointer until release.
func (b *scrollbar) update(gtx C, l *widget.List) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: b,
			Kinds:   pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Enter | pointer.Leave | pointer.Scroll,
			ScrollY: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20}})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Enter:
			b.hover = true
		case pointer.Leave:
			b.hover = false
		case pointer.Press:
			if b.n == 0 || !e.Buttons.Contain(pointer.ButtonPrimary) {
				continue
			}
			y := int(e.Position.Y)
			b.grab = y - b.top
			if b.grab < 0 || b.grab >= b.thumb {
				b.grab = b.thumb / 2
			}
			b.dragging = true
			b.seek(l, y)
		case pointer.Drag:
			if b.dragging {
				b.seek(l, int(e.Position.Y))
			}
		case pointer.Release, pointer.Cancel:
			b.dragging = false
		case pointer.Scroll:
			// The strip covers the list's own wheel handling; pass it on.
			if !b.dragging {
				l.Position.Offset += int(e.Scroll.Y)
				l.Position.BeforeEnd = true
			}
		}
	}
}

// seek scrolls the list so the thumb's top is at y - grab.
func (b *scrollbar) seek(l *widget.List, y int) {
	span := b.track - b.thumb
	if span <= 0 || b.est <= 0 {
		return
	}
	pad := (b.view - b.track) / 2
	b.t = min(1, max(0, float32(y-b.grab-pad)/float32(span)))
	if b.t >= 1 {
		// Past the last row; the list lays out the end from there.
		l.Position.First, l.Position.Offset, l.Position.BeforeEnd = b.n, 0, false
		return
	}
	s := b.t * (float32(b.n)*b.est - float32(b.view))
	first := int(s / b.est)
	l.Position.First = first
	l.Position.Offset = int(s - float32(first)*b.est)
	l.Position.BeforeEnd = true
}
