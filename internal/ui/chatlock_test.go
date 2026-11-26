package ui

import (
	"slices"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/mock"
)

// lockAnswer draws frames until the secret code's check has answered.
func lockAnswer(t *testing.T, u *UI, frame func()) {
	t.Helper()
	for i := 0; i < 500 && u.chatLock.checking; i++ {
		frame()
	}
	if u.chatLock.checking {
		t.Fatal("the secret code's check never answered")
	}
	frame()
}

// TestChatLock checks that a locked chat leaves the chat list and its
// search for Locked chats, which open with the secret code and lock again
// once left, and that opening a locked chat from elsewhere asks for it.
func TestChatLock(t *testing.T) {
	u, b, frame, _ := chatListUI(t)
	b.SetLocked("rina", true)
	frame()
	if slices.Contains(visibleIDs(u), "rina") {
		t.Fatal("a locked chat is in the chat list")
	}
	if !u.chatLock.row || u.listRow(0) != 2 {
		t.Fatalf("Locked chats row %v, first chat at row %d", u.chatLock.row, u.listRow(0))
	}
	u.sidebar.search.SetText("rina")
	frame()
	if slices.Contains(visibleIDs(u), "rina") {
		t.Error("the search finds a locked chat")
	}
	u.sidebar.search.SetText("")
	frame()

	// Opened from elsewhere (a notification), it asks for the code.
	u.open(u.chatByID("rina"))
	frame()
	if u.dialog.kind != dialogLockCode || u.selected != nil && u.selected.ID == "rina" {
		t.Fatalf("dialog %d, selected %v", u.dialog.kind, u.selected)
	}
	u.checkLockCode("0000", false)
	lockAnswer(t, u, frame)
	if !u.chatLock.wrong || u.sidebar.showLocked {
		t.Fatal("a wrong code opened Locked chats")
	}
	u.checkLockCode(mock.DemoLockCode, false)
	lockAnswer(t, u, frame)
	if !u.sidebar.showLocked || u.selected == nil || u.selected.ID != "rina" {
		t.Fatalf("after the code: Locked chats %v, selected %v", u.sidebar.showLocked, u.selected)
	}
	if got := visibleIDs(u); !slices.Equal(got, []string{"rina"}) {
		t.Errorf("Locked chats shows %v", got)
	}

	// Leaving locks them again: the chat closes.
	u.sidebar.back.Click()
	frame()
	frame()
	if u.sidebar.showLocked || u.chatLock.open || u.selected != nil {
		t.Errorf("after leaving: page %v, open %v, selected %v", u.sidebar.showLocked, u.chatLock.open, u.selected)
	}

	// Hidden by the phone: no row, and the code typed in the search opens them.
	b.HideLocked = true
	u.chatLock.read = false
	frame()
	if u.chatLock.row {
		t.Error("the hidden row shows")
	}
	u.sidebar.search.SetText(mock.DemoLockCode)
	frame()
	lockAnswer(t, u, frame)
	if !u.sidebar.showLocked || u.sidebar.search.Text() != "" {
		t.Errorf("the code in the search: Locked chats %v, search %q", u.sidebar.showLocked, u.sidebar.search.Text())
	}
}
