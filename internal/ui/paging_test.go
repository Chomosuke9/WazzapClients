package ui

import (
	"fmt"
	"image"
	"slices"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// longChat serves the demo data with a long history in one chat.
type longChat struct {
	*mock.Backend
	id   string
	msgs []*model.Message
}

func (b *longChat) Messages(chatID string, limit int) []*model.Message {
	if chatID != b.id {
		return b.Backend.Messages(chatID, limit)
	}
	return slices.Clone(b.msgs[max(0, len(b.msgs)-limit):])
}

func (b *longChat) MessagesBefore(chatID, id string, limit int) []*model.Message {
	i := b.index(id)
	if chatID != b.id || i < 0 {
		return b.Backend.MessagesBefore(chatID, id, limit)
	}
	return slices.Clone(b.msgs[max(0, i-limit):i])
}

func (b *longChat) MessagesFrom(chatID, id string, limit int) []*model.Message {
	i := b.index(id)
	if chatID != b.id || i < 0 {
		return b.Backend.MessagesFrom(chatID, id, limit)
	}
	return slices.Clone(b.msgs[i:min(len(b.msgs), i+limit)])
}

func (b *longChat) index(id string) int {
	return slices.IndexFunc(b.msgs, func(m *model.Message) bool { return m.ID == id })
}

// TestMessagePaging scrolls a long chat to its start and back, and checks
// that pages load at either end, the view stays put while they do, and no
// more than maxLoaded messages stay loaded.
func TestMessagePaging(t *testing.T) {
	const n = 1000
	b := &longChat{Backend: mock.New(), id: "rina"}
	start := time.Now().Add(-48 * time.Hour)
	for i := range n {
		b.msgs = append(b.msgs, &model.Message{
			ID: fmt.Sprintf("long-%d", i), ChatID: b.id, FromMe: i%3 == 0,
			Text: fmt.Sprintf("message %d", i), Time: start.Add(time.Duration(i) * 2 * time.Minute),
		})
	}
	u := New(b)
	u.Start(func() {})
	u.SelectID(b.id)
	now := time.Now()
	var ops op.Ops
	var r input.Router
	frame := func() {
		ops.Reset()
		u.Layout(layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1100, 700))})
		r.Frame(&ops)
		now = now.Add(time.Second)
	}
	wheel := func(dy float32) {
		r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(800, 350), Scroll: f32.Pt(0, dy)})
		frame()
	}
	// firstShown is the first message on screen and its distance from the top.
	firstShown := func() (string, int) {
		pos := u.conv.list.Position
		y := -pos.Offset
		for j, r := range u.conv.rows[pos.First:] {
			if r.msg != nil {
				return r.msg.ID, y
			}
			y += u.conv.heights[pos.First+j]
		}
		return "", 0
	}
	// shownAt is where message id is on screen, or just above it; ok is
	// false when it wasn't laid out.
	shownAt := func(id string) (y int, ok bool) {
		pos := u.conv.list.Position
		is := func(j int) bool { r := u.conv.rows[j]; return r.msg != nil && r.msg.ID == id }
		y = -pos.Offset
		for j := pos.First; j < len(u.conv.rows); j++ {
			if is(j) {
				return y, true
			}
			h, ok := u.conv.heights[j]
			if !ok {
				break
			}
			y += h
		}
		y = -pos.Offset
		for j := pos.First - 1; j >= 0; j-- {
			h, ok := u.conv.heights[j]
			if !ok {
				break
			}
			if y -= h; is(j) {
				return y, true
			}
		}
		return 0, false
	}
	// scroll turns the wheel by dy each frame while more is true, and
	// returns how many pages loaded. Every frame starts a new wheel notch,
	// so the list moves the same distance each frame, paging or not.
	scroll := func(dy float32, more func() bool) (pages int) {
		step := 0
		for i := 0; i < 1000 && more(); i++ {
			id, y := firstShown()
			ver := u.msgsVer
			wheel(dy)
			y2, ok := shownAt(id)
			switch {
			case u.msgsVer == ver:
				if ok && step == 0 {
					step = y2 - y
				}
			case step == 0:
				t.Fatal("paged before scrolling")
			default:
				pages++
				if !ok || y2 != y+step {
					t.Fatalf("page %d moved %s from y=%d to %d (shown %v), want %d", pages, id, y, y2, ok, y+step)
				}
			}
			if len(u.msgs) > maxLoaded {
				t.Fatalf("%d messages loaded", len(u.msgs))
			}
		}
		return pages
	}

	// Up to the start of the chat.
	pages := scroll(-150, func() bool { return u.conv.olderMore || u.conv.list.Position.First > 0 })
	if u.msgs[0].ID != "long-0" || u.conv.rows[0].kind != rowEncryption || !u.conv.newerMore {
		t.Fatalf("at the top: first %s, row 0 kind %v, newerMore %v", u.msgs[0].ID, u.conv.rows[0].kind, u.conv.newerMore)
	}
	if pages != (n-messagePage)/messagePage {
		t.Fatalf("%d pages loaded on the way up", pages)
	}

	// A new message isn't inserted past the loaded end...
	in := &model.Message{ID: "new-in", ChatID: b.id, Text: "hi", Time: time.Now()}
	u.upsertMessage(in)
	if slices.Contains(u.msgs, in) {
		t.Fatal("a new message was added to the old messages")
	}

	// ...and shows after scrolling back down to the newest.
	b.msgs = append(b.msgs, in)
	if pages := scroll(150, func() bool { return u.conv.newerMore || u.conv.list.Position.BeforeEnd }); pages != (n+1-maxLoaded+messagePage-1)/messagePage { // n+1: with the new one
		t.Fatalf("%d pages loaded on the way down", pages)
	}
	if last := u.msgs[len(u.msgs)-1]; last != in || !u.conv.list.ScrollToEnd {
		t.Fatalf("at the bottom: last %s, ScrollToEnd %v", last.ID, u.conv.list.ScrollToEnd)
	}

	// Jumping to an old message loads the messages around it.
	u.jumpTo("long-5")
	for range 3 {
		frame()
	}
	if u.msgs[0].ID != "long-0" || !u.conv.newerMore || u.conv.flash != "long-5" {
		t.Fatalf("after jumping: first %s, newerMore %v", u.msgs[0].ID, u.conv.newerMore)
	}
	if pos := u.conv.list.Position; pos.First > 10 {
		t.Fatalf("after jumping, list at %+v", pos)
	}

	// Sending from there goes back to the newest messages.
	out := &model.Message{ID: "new-out", ChatID: b.id, FromMe: true, Text: "yo", Time: time.Now()}
	u.upsertMessage(out)
	if last := u.msgs[len(u.msgs)-1]; last != out || u.conv.newerMore {
		t.Fatalf("after sending: last %s, newerMore %v", last.ID, u.conv.newerMore)
	}
}
