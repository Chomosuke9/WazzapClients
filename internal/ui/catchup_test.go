package ui

import (
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestCatchUpCard checks when the card that counts missed messages shows:
// only for many of them, and not right away.
func TestCatchUpCard(t *testing.T) {
	st := newSlashTest(t, "rina")
	u := st.u
	u.now = func() time.Time { return st.now }
	send := func(done, total int) { u.catchUp.event(model.CatchUpEvent{Done: done, Total: total}, st.now) }
	shown := func() bool {
		for range 80 {
			st.frame()
		}
		return u.catchUp.anim.v > 0
	}
	send(0, catchUpMin-1)
	if shown() {
		t.Fatal("shows for a few messages")
	}
	send(0, 0)
	send(0, 300)
	st.frame()
	if u.catchUp.anim.v > 0 {
		t.Fatal("shows right away")
	}
	if !shown() {
		t.Fatal("doesn't show for many messages")
	}
	send(300, 300)
	if shown() {
		t.Fatal("stays once they all came")
	}
	send(0, 300)
	send(100, 300)
	if !shown() {
		t.Fatal("doesn't show again")
	}
	send(0, 0)
	if shown() {
		t.Fatal("stays after the connection was lost")
	}
	if u.catchUp.done != 100 {
		t.Fatalf("fades out at %d", u.catchUp.done)
	}
}

// TestSyncDialog checks that the banner opens the dialog while older
// messages sync, and that the dialog outlives the sync.
func TestSyncDialog(t *testing.T) {
	st := newSlashTest(t, "rina")
	u := st.u
	u.sync = model.SyncEvent{Stage: model.SyncChats, Percent: 10}
	u.btn("syncbanner").Click()
	st.frame()
	if u.dialog.isOpen() {
		t.Fatal("opens while the chats sync")
	}
	u.sync = model.SyncEvent{Stage: model.SyncOlder, Percent: 10}
	st.frame()
	u.btn("syncbanner").Click()
	st.frame()
	if !u.dialog.isOpen() || u.dialog.kind != dialogSync {
		t.Fatal("the banner doesn't open the dialog")
	}
	u.sync = model.SyncEvent{Stage: model.SyncOlder, Percent: 100, Done: true}
	st.frame()
	if !u.dialog.isOpen() {
		t.Fatal("closes as the sync ends")
	}
	u.btn("dialog:1").Click()
	st.frame()
	if u.dialog.isOpen() {
		t.Fatal("OK doesn't close it")
	}
}
