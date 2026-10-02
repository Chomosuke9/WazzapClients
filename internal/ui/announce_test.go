package ui

import (
	"testing"

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
