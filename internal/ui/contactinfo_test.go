package ui

import (
	"image"
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

// TestContactInfoBack checks that a member's contact info opened from a
// group's info returns to it, and that opening another chat closes it.
func TestContactInfoBack(t *testing.T) {
	u := New(mock.NewReference())
	u.Start(func() {})
	u.SelectID("test@g.us")
	u.openInfo("test@g.us")
	u.openContact("vivy@lid", "Vivy")
	if u.info.chatID != "vivy@lid" || len(u.info.back) != 1 {
		t.Fatalf("showing %q with %d pages back, want vivy@lid with 1", u.info.chatID, len(u.info.back))
	}
	if u.info.data == nil || u.info.data.Business == nil {
		t.Fatal("the business profile didn't load")
	}
	u.infoBack()
	if u.info.chatID != "test@g.us" || len(u.info.back) != 0 || !u.info.open {
		t.Fatalf("back shows %q (open %v), want the group's info", u.info.chatID, u.info.open)
	}
	u.openContact("vivy@lid", "Vivy")
	u.SelectID("alif@lid")
	if u.info.open {
		t.Error("the panel stayed open on another chat")
	}
}

// TestSenderOpensContact clicks a sender's name or picture in a group and
// expects their contact info.
func TestSenderOpensContact(t *testing.T) {
	u := New(mock.NewReference())
	u.Start(func() {})
	u.SelectID("test@g.us")
	now := testNow()
	var ops op.Ops
	var r input.Router
	frame := func() {
		ops.Reset()
		u.Layout(layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1100, 700))})
		r.Frame(&ops)
		now = now.Add(time.Second) // past double-click time and animations
	}
	for range 3 {
		frame()
	}
	// The newest messages, at the bottom of the conversation, are Vivy's.
	for y := 640; y > 300; y -= 6 {
		for x := 460; x < 640; x += 10 {
			p := f32.Pt(float32(x), float32(y))
			r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p},
				pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: p, Buttons: pointer.ButtonPrimary},
				pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p})
			frame()
			frame()
			if u.info.open {
				if u.info.chatID != "vivy@lid" {
					t.Fatalf("a click at %v opened %q's info", p, u.info.chatID)
				}
				return
			}
		}
	}
	t.Fatal("no click on the conversation opened the sender's contact info")
}

// TestHoursNow checks opening hours, including hours past midnight.
func TestHoursNow(t *testing.T) {
	biz := &model.Business{TimeZone: "UTC", Hours: []model.BusinessHours{
		{Day: time.Monday, Mode: "specific_hours", Open: 22 * 60, Close: 2 * 60},
		{Day: time.Tuesday, Mode: "specific_hours", Open: 9 * 60, Close: 17 * 60},
	}}
	at := func(day, h int) time.Time { return time.Date(2026, 9, 28+day, h, 30, 0, 0, time.UTC) } // 28 Sep 2026 is a Monday
	for _, c := range []struct {
		t    time.Time
		open bool
	}{
		{at(0, 21), false}, {at(0, 23), true}, {at(1, 1), true}, {at(1, 3), false},
		{at(1, 10), true}, {at(1, 18), false}, {at(2, 1), false},
	} {
		if got := hoursNow(biz, c.t).open; got != c.open {
			t.Errorf("open at %v = %v, want %v", c.t, got, c.open)
		}
	}
}
