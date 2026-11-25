package ui

import (
	"image"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
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

	from barGlide
}

// barGlide is a thumb on its way from where it was when the rows changed.
type barGlide struct {
	on         bool
	top, thumb int
	start      time.Time // the first frame of it
}

// glide makes the thumb glide from where it is to its place among other
// rows, instead of jumping there.
func (b *scrollbar) glide() {
	if b.n > 0 && !b.dragging && perf.animations {
		b.from = barGlide{on: true, top: b.top, thumb: b.thumb}
	}
}

// scrollList lays out a vertical list with WhatsApp's thin overlay scrollbar
// and smooth wheel scrolling.
func (u *UI) scrollList(gtx C, l *widget.List, n int, el layout.ListElement) D {
	return u.wheelList(gtx, &l.List, func(gtx C) D { return u.barList(gtx, l, n, el) })
}

// bar is list l's scrollbar.
func (u *UI) bar(l *widget.List) *scrollbar {
	if u.bars == nil {
		u.bars = make(map[*widget.List]*scrollbar)
	}
	b := u.bars[l]
	if b == nil {
		b = &scrollbar{}
		u.bars[l] = b
	}
	return b
}

// barList lays out list l with the scrollbar over it.
func (u *UI) barList(gtx C, l *widget.List, n int, el layout.ListElement) D {
	b := u.bar(l)
	if b.update(gtx, l) {
		b.seek(l)
	}

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
	b.draw(gtx, u.pal, dims.Size, n, content, t)
	return dims
}

// draw draws the scrollbar of a list of n rows, size on screen, whose
// content is about content px tall, with the thumb t (0 to 1) of the way
// down. A thumb being dragged keeps its size.
func (b *scrollbar) draw(gtx C, p *Palette, size image.Point, n int, content, t float32) {
	w, view := size.X, size.Y
	pad := gtx.Dp(2)
	track := view - 2*pad
	thumb := min(track, max(gtx.Dp(32), int(float32(track)*float32(view)/content)))
	if b.dragging && b.thumb > 0 {
		thumb = min(track, b.thumb)
	}
	top := pad + int(t*float32(track-thumb)+0.5)
	if f := &b.from; f.on {
		if f.start.IsZero() {
			f.start = gtx.Now
		}
		v := float32(gtx.Now.Sub(f.start)) / float32(durGrow)
		if b.dragging || gtx.Now.IsZero() || v >= 1 {
			*f = barGlide{}
		} else {
			e := easeOut(max(0, v))
			top, thumb = lerpInt(f.top, top, e), lerpInt(f.thumb, thumb, e)
			gtx.Execute(op.InvalidateCmd{})
		}
	}
	b.n, b.view, b.track, b.thumb, b.top = n, view, track, thumb, top

	// The hit area is wider than the thumb.
	hit := image.Rect(w-gtx.Dp(10), 0, w, view)
	area := clip.Rect(hit).Push(gtx.Ops)
	event.Op(gtx.Ops, b)
	area.Pop()

	col := p.TextSecondary
	col.A = 0x50
	if b.hover || b.dragging {
		col.A = 0x90
	}
	bw := gtx.Dp(5)
	x := w - pad - bw
	fillRRect(gtx, image.Rect(x, top, x+bw, top+thumb), gtx.Dp(3), col)
}

// update handles the pointer: pressing the thumb grabs it; pressing the
// track grabs the thumb by its middle there. Either way it then follows
// the pointer until release. It reports whether the pointer moved the thumb,
// to b.t: the list is to follow.
func (b *scrollbar) update(gtx C, l *widget.List) (moved bool) {
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
			moved = b.follow(y) || moved
		case pointer.Drag:
			if b.dragging {
				moved = b.follow(int(e.Position.Y)) || moved
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
	return moved
}

// follow puts the thumb's top at y - grab. It reports whether it can.
func (b *scrollbar) follow(y int) bool {
	span := b.track - b.thumb
	if span <= 0 {
		return false
	}
	pad := (b.view - b.track) / 2
	b.t = min(1, max(0, float32(y-b.grab-pad)/float32(span)))
	return true
}

// seek scrolls the list to the thumb's place.
func (b *scrollbar) seek(l *widget.List) {
	if b.est <= 0 {
		return
	}
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
