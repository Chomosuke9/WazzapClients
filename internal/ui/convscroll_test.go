package ui

import (
	"fmt"
	"image"
	"math"
	"strings"
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

// barTest is a chat of n messages of 1 to so many lines, whose scrollbar
// is dragged.
type barTest struct {
	*testing.T
	u   *UI
	b   *longChat
	now time.Time
	ops op.Ops
	r   input.Router
	x   float32 // of the scrollbar, in the window
	top float32 // of the list
}

func newBarTest(t *testing.T, n, lines int) *barTest {
	b := &longChat{Backend: mock.New(), id: "rina"}
	start := testNow().Add(-time.Duration(n) * 10 * time.Minute)
	for i := range n {
		b.msgs = append(b.msgs, &model.Message{
			ID: fmt.Sprintf("long-%d", i), ChatID: b.id, FromMe: i%3 == 0,
			Text: fmt.Sprintf("message %d", i) + strings.Repeat("\nline", i*7%lines), Time: start.Add(time.Duration(i) * 10 * time.Minute),
		})
	}
	bt := &barTest{T: t, u: New(b), b: b, now: testNow()}
	bt.u.Start(func() {})
	bt.u.SelectID(b.id)
	for range 90 {
		bt.frame()
	}
	// Find the bar: press at the window's right edge and down.
	bar := bt.bar()
	for y := float32(300); y < 700 && bt.x == 0; y += 10 {
		bt.mouse(pointer.Press, 1098, y)
		if bar.dragging {
			bt.x = 1098
			bt.top = y - float32(bar.grab+bar.top)
		}
		bt.mouse(pointer.Release, 1098, y)
	}
	if bt.x == 0 {
		t.Fatal("no scrollbar")
	}
	bt.u.loadLatest()
	for range 10 {
		bt.frame()
	}
	return bt
}

func (bt *barTest) bar() *scrollbar { return bt.u.bar(&bt.u.conv.list) }

func (bt *barTest) frame() {
	bt.ops.Reset()
	bt.u.Layout(layout.Context{Ops: &bt.ops, Now: bt.now, Source: bt.r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(1100, 700))})
	bt.r.Frame(&bt.ops)
	bt.now = bt.now.Add(16 * time.Millisecond)
}

func (bt *barTest) mouse(kind pointer.Kind, x, y float32) {
	bt.r.Queue(pointer.Event{Kind: kind, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(x, y)})
	bt.frame()
}

// grab presses the thumb in its middle and returns where.
func (bt *barTest) grab() float32 {
	b := bt.bar()
	y := bt.top + float32(b.top+b.thumb/2)
	bt.mouse(pointer.Press, bt.x, y)
	if !b.dragging || b.grab != b.thumb/2 {
		bt.Fatalf("didn't grab the thumb: %+v", b)
	}
	return y
}

// msgAt is the index of the message y px down the list, with its fraction.
func (bt *barTest) msgAt(y int) float64 {
	bt.Helper()
	u := bt.u
	pos := u.conv.list.Position
	top := -pos.Offset
	for i := pos.First; i < len(u.conv.rows); i++ {
		h := u.conv.heights[i]
		if m := u.conv.rows[i].msg; m != nil && top+h > y {
			return float64(bt.b.index(m.ID)) + max(0, float64(y-top)/float64(h))
		}
		top += h
	}
	return float64(len(bt.b.msgs))
}

// release lets the thumb go and waits for what that loads.
func (bt *barTest) release() {
	bt.mouse(pointer.Release, bt.x, bt.top+float32(bt.bar().top))
	for range 30 {
		bt.frame()
	}
}

// TestConvBarDrag drags the thumb of a long chat to the top, over and over:
// the list follows it evenly, no page loads until it's let go, and then one
// does, with the messages on screen staying where they are.
func TestConvBarDrag(t *testing.T) {
	const n = 1000
	bt := newBarTest(t, n, 12)
	b := bt.bar()
	if b.top+b.thumb != b.view-2 {
		t.Fatalf("at the newest message, thumb ends at %d of %d", b.top+b.thumb, b.view)
	}
	for pull := 1; ; pull++ {
		if pull > 15 {
			t.Fatalf("not at the chat's start after %d pulls: %s", pull, bt.u.msgs[0].ID)
		}
		first, loaded, ver := bt.u.msgs[0].ID, len(bt.u.msgs), bt.u.msgsVer
		y := bt.grab()
		last := bt.msgAt(0)
		for b.t > 0 {
			y--
			bt.mouse(pointer.Move, bt.x, y)
			at := bt.msgAt(0)
			// A pixel is a message or so, and the date chips' rows.
			if at > last+0.01 || last-at > 4 {
				t.Fatalf("pull %d: a pixel up went from message %.2f to %.2f", pull, last, at)
			}
			last = at
			if got := bt.top + float32(b.top+b.thumb/2); math.Abs(float64(got-y)) > 1 {
				t.Fatalf("pull %d: thumb at %v, the pointer at %v", pull, got, y)
			}
		}
		// Held at the top, however the pointer shakes.
		for i := range 20 {
			bt.mouse(pointer.Move, bt.x, y-float32(i%3))
		}
		if pos := bt.u.conv.list.Position; bt.u.msgsVer != ver || pos.First != 0 || pos.Offset != 0 {
			t.Fatalf("pull %d: held at the top, list at %+v, %s loaded first, was %s", pull, pos, bt.u.msgs[0].ID, first)
		}
		if first == "long-0" {
			break
		}
		at := bt.msgAt(100)
		bt.release()
		if got := bt.b.index(first) - bt.b.index(bt.u.msgs[0].ID); got != messagePage {
			t.Fatalf("pull %d of %d messages loaded %d", pull, loaded, got)
		}
		// The topmost date chip goes when the day's earlier messages come.
		if got := bt.msgAt(100); math.Abs(got-at) > 1.5 {
			t.Fatalf("pull %d: let go, message %.2f to %.2f", pull, at, got)
		}
	}
	// And back down to the newest.
	bt.release()
	for pull := 1; bt.u.conv.newerMore; pull++ {
		if pull > 15 {
			t.Fatal("never at the newest message")
		}
		bt.grab()
		bt.mouse(pointer.Move, bt.x, bt.top+float32(b.view)+50)
		bt.release()
	}
	bt.grab()
	bt.mouse(pointer.Move, bt.x, bt.top+float32(b.view)+50)
	bt.release()
	if pos := bt.u.conv.list.Position; pos.BeforeEnd || !bt.u.conv.list.ScrollToEnd || b.top+b.thumb != b.view-2 {
		t.Fatalf("at the bottom: %+v, thumb %d", pos, b.top)
	}
}

// TestConvBarStays checks that letting the thumb go away from the ends
// moves neither it nor the list.
func TestConvBarStays(t *testing.T) {
	bt := newBarTest(t, 400, 12)
	b := bt.bar()
	bt.grab()
	bt.mouse(pointer.Move, bt.x, bt.top+float32(b.view)*2/3)
	at, top := bt.msgAt(0), b.top
	bt.release()
	if abs(b.top-top) > 1 || bt.msgAt(0) != at {
		t.Fatalf("thumb from %d to %d, message %.2f to %.2f", top, b.top, at, bt.msgAt(0))
	}
}

// TestConvBarWheel checks that the thumb moves evenly while the wheel goes
// up through uneven rows, and glides when a page loads.
func TestConvBarWheel(t *testing.T) {
	bt := newBarTest(t, 600, 12)
	b := bt.bar()
	last, thumb, ver := b.top, b.thumb, bt.u.msgsVer
	glide, pages := 0, 0
	for i := 0; bt.u.conv.olderMore || bt.u.conv.list.Position.First > 0; i++ {
		if i > 20000 {
			t.Fatal("never at the top")
		}
		if i%4 == 0 {
			bt.r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(800, 350), Scroll: f32.Pt(0, -150)})
		}
		bt.frame()
		if bt.u.msgsVer != ver {
			ver, glide = bt.u.msgsVer, 14
			pages++
			if b.top != last || b.thumb != thumb {
				t.Fatalf("page %d: thumb jumped from %d to %d", pages, last, b.top)
			}
		}
		if glide--; glide < 0 && (b.top > last+1 || last-b.top > 12 || abs(b.thumb-thumb) > 4) {
			t.Fatalf("thumb from %d (%d px) to %d (%d px) in a frame", last, thumb, b.top, b.thumb)
		}
		last, thumb = b.top, b.thumb
	}
	if pages != 5 {
		t.Fatalf("%d pages", pages)
	}
	for range 60 {
		bt.frame()
	}
	if b.top != 2 {
		t.Fatalf("thumb at %d at the top", b.top)
	}
}

// TestConvBarShort drags through a chat that is all loaded.
func TestConvBarShort(t *testing.T) {
	bt := newBarTest(t, 40, 12)
	b := bt.bar()
	y := bt.grab()
	last := bt.msgAt(0)
	for b.t > 0 {
		y--
		bt.mouse(pointer.Move, bt.x, y)
		at := bt.msgAt(0)
		if at > last || last-at > 1.5 {
			t.Fatalf("a pixel went from message %.2f to %.2f", last, at)
		}
		last = at
	}
	if pos := bt.u.conv.list.Position; pos.First != 0 || pos.Offset != 0 {
		t.Fatalf("at the top: %+v", pos)
	}
}

// TestConvBarSize checks that the thumb of a long chat shrinks with each
// page that loads, and that it doesn't open with one for its first page.
func TestConvBarSize(t *testing.T) {
	bt := newBarTest(t, 1000, 1)
	u := bt.u
	last := u.barRows()
	if last < messagePage*3/2 || last >= maxLoaded {
		t.Fatalf("opened with a thumb for %d rows", last)
	}
	for ver := u.msgsVer; len(u.msgs) < maxLoaded; {
		bt.r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(800, 350), Scroll: f32.Pt(0, -300)})
		bt.frame()
		if n := u.barRows(); u.msgsVer != ver {
			if n <= last {
				t.Fatalf("%d messages loaded: a thumb for %d rows, was %d", len(u.msgs), n, last)
			}
			ver, last = u.msgsVer, n
		}
	}
	// All of a short chat loaded, it's sized for that.
	bt = newBarTest(t, 150, 1)
	u = bt.u
	for u.conv.olderMore {
		bt.r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(800, 350), Scroll: f32.Pt(0, -300)})
		bt.frame()
	}
	if n := u.conv.scroll.total; u.barRows() != n {
		t.Fatalf("all loaded: a thumb for %d of %d rows", u.barRows(), n)
	}
}
