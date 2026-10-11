package ui

import (
	"image"
	"slices"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
)

// TestAnnouncementsSendBlocked checks who may post in a community's
// announcements: its admins, and members only read them.
func TestAnnouncementsSendBlocked(t *testing.T) {
	u := New(mock.NewReference())
	u.Start(func() {})
	for _, tc := range []struct {
		chat, want string
		ann        bool
	}{
		{"fpam-ann@g.us", "community admins", true}, // you're a member
		{"wa-ann@g.us", "", true},                   // you're an admin
		{"test@g.us", "", false},
	} {
		c := u.chatByID(tc.chat)
		if c == nil {
			t.Fatalf("no chat %s", tc.chat)
		}
		if got := u.announcementsOf(c) != nil; got != tc.ann {
			t.Errorf("%s: announcements %v, want %v", tc.chat, got, tc.ann)
		}
		if got := u.sendBlocked(c); got != tc.want {
			t.Errorf("%s: sendBlocked %q, want %q", tc.chat, got, tc.want)
		}
	}
}

// TestLeftGroup checks that a group you're no longer a member of can't be
// sent to, and offers to delete it instead of exiting it.
func TestLeftGroup(t *testing.T) {
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	u.SelectID("family")
	if got := u.sendBlocked(u.selected); got != "" {
		t.Fatalf("a group you're in: sendBlocked %q", got)
	}
	b.LeaveGroup("family")
	var ops op.Ops
	u.Layout(layout.Context{Ops: &ops, Now: testNow(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(1400, 900))})
	c := u.chatByID("family")
	if !c.Left {
		t.Fatal("leaving didn't mark the group left")
	}
	if got := u.sendBlocked(c); got != blockedLeft {
		t.Errorf("a group you left: sendBlocked %q, want %q", got, blockedLeft)
	}
	var labels []string
	for _, it := range u.convMenuItems(c) {
		labels = append(labels, it.label)
	}
	if !slices.Contains(labels, "Delete group") || slices.Contains(labels, "Exit group") ||
		slices.Contains(labels, "Add member") || slices.Contains(labels, "Disappearing messages") {
		t.Errorf("a left group's menu %q", labels)
	}
}
