package ui

import (
	"image"
	"testing"

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
		u.Layout(layout.Context{Ops: &ops, Now: testNow(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
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

// TestOpenChannelStaysRead checks the same for a channel, whose open chat
// is a copy of the channel.
func TestOpenChannelStaysRead(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.page = pageChannels
	u.applyEvents()
	if len(u.channels) == 0 {
		t.Skip("no demo channels")
	}
	ch := u.channels[0]
	u.open(channelChat(ch))
	ch.Unread = 3 // a new post arrived
	u.markSeen()
	if ch.Unread != 0 {
		t.Errorf("open channel shows %d unread", ch.Unread)
	}
}

// TestUnreadDivider checks that opening a chat with unread messages puts
// "N unread messages" above the first of them and scrolls there, and that
// sending a message takes it away.
func TestUnreadDivider(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("work") // 3 unread
	c := u.selected
	rows := u.rows(c)
	at := -1
	for i, r := range rows {
		if r.kind == rowUnread {
			at = i
		}
	}
	if at < 0 || at+1 >= len(rows) {
		t.Fatal("no divider")
	}
	if u.conv.unread.n != 3 {
		t.Errorf("divider counts %d, want 3", u.conv.unread.n)
	}
	// The newest 3 incoming messages are below it.
	in := 0
	for _, r := range rows[at+1:] {
		if r.kind == rowMessage && !r.msg.FromMe {
			in++
		}
	}
	if in != 3 || !rows[at+1].first {
		t.Errorf("%d incoming messages below the divider (first %v), want 3", in, rows[at+1].first)
	}
	if p := u.conv.scrollTo; p == nil || p.First != at {
		t.Errorf("scrolls to %+v, want row %d", p, at)
	}

	u.upsertMessage(u.backend.Send(c.ID, model.Draft{Text: "on it"}))
	for _, r := range u.rows(c) {
		if r.kind == rowUnread {
			t.Error("the divider stayed after sending")
		}
	}

	u.SelectID("budi") // nothing unread
	for _, r := range u.rows(u.selected) {
		if r.kind == rowUnread {
			t.Error("a divider in a read chat")
		}
	}
}
