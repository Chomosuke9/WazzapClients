package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestOpenChatStaysRead checks that messages arriving in the open chat
// don't leave an unread badge, unless the window is away.
func TestOpenChatStaysRead(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("rina")
	var ops op.Ops
	frame := func() {
		ops.Reset()
		u.Layout(layout.Context{Ops: &ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1100, 700))})
	}
	frame()
	arrive := func(n int) *model.Chat {
		// The backend's chat update, counted before the chat was marked read.
		c := *u.selected
		c.Unread = n
		u.upsertChat(&c)
		frame()
		return u.chatByID(c.ID)
	}
	if c := arrive(2); c.Unread != 0 {
		t.Errorf("open chat shows %d unread", c.Unread)
	}
	u.away = true
	if c := arrive(1); c.Unread != 1 {
		t.Errorf("away: open chat shows %d unread, want 1", c.Unread)
	}
	u.away = false
	frame()
	if c := u.chatByID("rina"); c.Unread != 0 {
		t.Errorf("back: open chat shows %d unread", c.Unread)
	}
}
