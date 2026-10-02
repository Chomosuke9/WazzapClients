package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestMarkAllRead marks every demo chat read, and the ⋮ menu's Starred
// messages lists the starred messages of every chat.
func TestMarkAllRead(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	now := testNow()
	var ops op.Ops
	var r input.Router
	frame := func() {
		ops.Reset()
		u.Layout(layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1100, 700))})
		r.Frame(&ops)
		now = now.Add(100 * time.Millisecond)
	}
	frame()
	unread := 0
	for _, c := range u.chats {
		if c.Unread != 0 {
			unread++
		}
	}
	if unread == 0 {
		t.Fatal("no unread demo chats")
	}
	u.markAllRead()
	frame()
	for _, c := range u.chats {
		if c.Unread != 0 {
			t.Errorf("%s still has %d unread", c.Name, c.Unread)
		}
	}

	var starred []*model.Message
	for _, m := range u.msgs[max(0, len(u.msgs)-2):] {
		u.backend.Star(m, true)
		starred = append(starred, m)
	}
	u.openStarred()
	frame()
	if got := len(u.gallery.list.msgs); got != len(starred) {
		t.Errorf("Starred messages lists %d messages, want %d", got, len(starred))
	}
	// The rail's Media button turns the panel to media, not closes it.
	u.openGallery("", "")
	if !u.gallery.open || u.gallery.tab != model.GalleryMedia {
		t.Errorf("Media from starred: open %v, tab %v", u.gallery.open, u.gallery.tab)
	}
}
