package ui

import (
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
)

// chatListUI starts a demo UI and returns it with a function that draws a
// frame of a 1280x800 window.
func chatListUI(t *testing.T) (*UI, *input.Router, func()) {
	t.Helper()
	u := New(mock.New())
	u.Start(func() {})
	now := testNow()
	var ops op.Ops
	r := new(input.Router)
	frame := func() {
		ops.Reset()
		u.Layout(layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1280, 800))})
		r.Frame(&ops)
		now = now.Add(100 * time.Millisecond)
	}
	frame()
	return u, r, frame
}

// TestChatRowMenu checks the chat row's menu follows WhatsApp's: Block for
// a contact, and no Close chat even for the open chat.
func TestChatRowMenu(t *testing.T) {
	u, _, _ := chatListUI(t)
	u.SelectID("rina")
	var labels []string
	for _, it := range u.chatMenuItems(u.chatByID("rina")) {
		labels = append(labels, it.label)
	}
	want := []string{"Archive chat", "Unmute notifications", "Unpin chat", "Mark as unread", "Add to favourites",
		"Add to list", "", "Block", "Clear chat", "Delete chat"}
	// The demo's Rina may be muted or not, favourite or not, unread or not:
	// compare the order of what's there.
	var got []string
	for _, l := range labels {
		switch l {
		case "Mute notifications":
			l = "Unmute notifications"
		case "Pin chat":
			l = "Unpin chat"
		case "Mark as read":
			l = "Mark as unread"
		case "Remove from favourites":
			l = "Add to favourites"
		}
		got = append(got, l)
	}
	if !slices.Equal(got, want) {
		t.Errorf("Rina's menu is %q, want %q", labels, want)
	}
	for _, it := range u.chatMenuItems(u.chatByID("family")) {
		if it.label == "Block" || it.label == "Close chat" {
			t.Errorf("a group's menu offers %q", it.label)
		}
	}
}

// TestSelectChats picks chats and runs the select bar's actions on them.
func TestSelectChats(t *testing.T) {
	u, _, frame := chatListUI(t)
	u.startChatSelect()
	frame()
	u.pickRow("budi")
	u.pickRow("sari")
	u.pickAction("pin")
	frame()
	if u.sidebar.pick.on {
		t.Error("select mode is still on after Pin")
	}
	for _, id := range []string{"budi", "sari"} {
		if !u.chatByID(id).Pinned {
			t.Errorf("%s isn't pinned", id)
		}
	}

	u.startChatSelect()
	u.pickRow("budi")
	u.pickRow("sari")
	u.pickAction("archive")
	frame()
	for _, id := range []string{"budi", "sari"} {
		if !u.chatByID(id).Archived {
			t.Errorf("%s isn't archived", id)
		}
	}

	// Mute asks how long first; mark read takes the unread chats.
	u.startChatSelect()
	u.pickRow("rina")
	u.pickRow("work")
	u.pickAction("mute")
	if u.ctx.kind != ctxPickMute || !u.sidebar.pick.on {
		t.Fatalf("Mute opened menu %v, select mode %v", u.ctx.kind, u.sidebar.pick.on)
	}
	u.pickMuteItems()[2].run() // Mute always
	frame()
	if !u.chatByID("rina").Muted || !u.chatByID("work").Muted {
		t.Error("Mute always didn't mute both chats")
	}
	u.startChatSelect()
	u.pickRow("work")
	if u.chatByID("work").Unread == 0 {
		t.Fatal("the demo's work chat isn't unread")
	}
	u.pickAction("read")
	frame()
	if n := u.chatByID("work").Unread; n != 0 {
		t.Errorf("work still has %d unread", n)
	}

	// Delete leaves groups alone.
	u.startChatSelect()
	u.pickRow("bank")
	u.pickRow("design")
	u.pickAction("delete")
	if u.dialog.kind != dialogConfirm {
		t.Fatal("Delete didn't ask")
	}
	u.dialog.buttons[0].run()
	frame()
	if u.chatByID("bank") != nil {
		t.Error("bank wasn't deleted")
	}
	if u.chatByID("design") == nil {
		t.Error("a group was deleted")
	}

	// Escape leaves select mode.
	u.dialog = dialogState{}
	u.startChatSelect()
	u.Escape()
	if u.sidebar.pick.on {
		t.Error("Escape didn't leave select mode")
	}
}

// TestChipsScroll checks the filter chips scroll sideways when the lists'
// chips don't fit, and that a list's chip filters on it.
func TestChipsScroll(t *testing.T) {
	u, r, frame := chatListUI(t)
	u.split.w = 320
	frame()
	frame()
	if len(u.sidebar.chipRow.lists) == 0 {
		t.Fatal("no demo lists")
	}
	p := f32.Pt(float32(railWidth+100), 41+1+68+43+11+17) // over the chips, below the title bar
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p},
		pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: p, Scroll: f32.Pt(0, 120)})
	frame()
	frame()
	if u.sidebar.chipRow.off <= 0 {
		t.Errorf("the wheel didn't scroll the chips (offset %d)", u.sidebar.chipRow.off)
	}

	l := u.sidebar.chipRow.lists[0]
	u.sidebar.filter, u.sidebar.listID = filterList, l.ID
	u.listFilterChats()
	frame()
	if len(u.sidebar.visible) != len(l.Chats) {
		t.Errorf("list %s shows %d chats, want %d", l.Name, len(u.sidebar.visible), len(l.Chats))
	}
}

// TestAppLock sets a password, locks, unlocks with it and turns it off.
func TestAppLock(t *testing.T) {
	stored := hashPassword("secret1")
	if !checkPassword(stored, "secret1") || checkPassword(stored, "secret2") || checkPassword("", "") {
		t.Fatal("checkPassword got it wrong")
	}
	if hashPassword("secret1") == stored {
		t.Error("two hashes of a password are the same: no salt")
	}

	u, _, frame := chatListUI(t)
	if u.lock.locked {
		t.Fatal("locked without a password")
	}
	u.backend.SetPref(prefAppLock, stored)
	u.backend.SetPref(prefAppLockAfter, "1")
	u.touchLock()
	frame()
	if u.lock.locked {
		t.Fatal("locked at once")
	}
	// A minute without input locks it.
	u.now = func() time.Time { return testNow().Add(2 * time.Minute) }
	frame()
	if !u.lock.locked {
		t.Fatal("didn't lock itself after a minute")
	}
	u.lock.pw.SetText("wrong!")
	u.btn("lock:unlock").Click()
	frame()
	if !u.lock.locked || !u.lock.wrong {
		t.Fatal("a wrong password unlocked it")
	}
	u.lock.pw.SetText("secret1")
	u.btn("lock:unlock").Click()
	frame()
	if u.lock.locked {
		t.Fatal("the password didn't unlock it")
	}
	// A new UI (a new window) starts locked.
	if v := New(u.backend); !v.lock.locked {
		t.Error("a new UI isn't locked")
	}
	// Turned off, the lock goes.
	u.lockNow()
	u.backend.SetPref(prefAppLock, "")
	frame()
	if u.lock.locked {
		t.Error("still locked with App lock off")
	}
}
