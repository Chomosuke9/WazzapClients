package wa

import (
	"fmt"
	"testing"
	"time"

	waBinary "github.com/polymorfa/hypermeow/binary"
	"github.com/polymorfa/hypermeow/proto/waHistorySync"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// polled returns the events of type T since the last poll.
func polled[T model.Event](b *Backend) []T {
	var out []T
	for _, ev := range b.Poll() {
		if e, ok := ev.(T); ok {
			out = append(out, e)
		}
	}
	return out
}

func offlineStanza(tag, child string, attrs waBinary.Attrs) *waBinary.Node {
	return &waBinary.Node{Tag: tag, Content: []waBinary.Node{{Tag: child, Attrs: attrs}}}
}

// TestCatchUp checks that missed messages are counted as they're handled,
// that those without an event don't hold the count back, and how it ends.
func TestCatchUp(t *testing.T) {
	b := testBackend(t)
	now := time.Unix(1_700_000_000, 0)
	b.clock = func() time.Time { return now }
	raw := func(n *waBinary.Node) { b.rawNodes(b.ctx, n) }
	arrive := func(id string, offline bool) {
		attrs := waBinary.Attrs{"id": id}
		if offline {
			attrs["offline"] = "0"
		}
		raw(&waBinary.Node{Tag: "message", Attrs: attrs})
	}
	handled := func(id string) {
		now = now.Add(time.Second)
		b.caughtUpTo(id)
	}
	want := func(want ...model.CatchUpEvent) {
		t.Helper()
		if got := polled[model.CatchUpEvent](b); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	preview := offlineStanza("ib", "offline_preview", waBinary.Attrs{"count": "9", "message": "4", "receipt": "5"})

	arrive("early", true) // no catch-up runs
	handled("early")
	want()

	raw(preview)
	want(model.CatchUpEvent{Total: 4})
	arrive("a", true)
	arrive("live", false)
	arrive("b", true) // brings no event
	arrive("c", true)
	want() // arrived, not handled
	handled("a")
	want(model.CatchUpEvent{Done: 1, Total: 4})
	handled("live")
	want()
	handled("c")
	want(model.CatchUpEvent{Done: 3, Total: 4})
	b.caughtUpTo("c") // again
	want()
	b.handle(&events.OfflineSyncCompleted{Count: 9})
	want(model.CatchUpEvent{Done: 4, Total: 4})

	// The connection drops midway, and the next one starts over.
	raw(preview)
	arrive("a", true)
	b.handle(&events.Disconnected{})
	want(model.CatchUpEvent{Total: 4}, model.CatchUpEvent{})
	handled("a")
	want()

	// Events that come too close together are left out.
	raw(preview)
	arrive("a", true)
	b.caughtUpTo("a")
	want(model.CatchUpEvent{Total: 4})

	// Nothing but receipts missed.
	b.handle(&events.Disconnected{})
	b.Poll()
	raw(offlineStanza("ib", "offline_preview", waBinary.Attrs{"count": "5", "message": "0", "receipt": "5"}))
	want()
}

// TestCatchUpMessage checks that a message's event counts it.
func TestCatchUpMessage(t *testing.T) {
	b := testBackend(t)
	b.startCatchUp(2)
	b.noteOffline(&waBinary.Node{Tag: "message", Attrs: waBinary.Attrs{"id": "W1", "offline": "0"}})
	b.handle(&events.UndecryptableMessage{Info: types.MessageInfo{ID: "W1", Timestamp: time.Unix(1_700_000_000, 0),
		MessageSource: types.MessageSource{Chat: types.NewJID("123", types.GroupServer), Sender: types.NewJID("555", types.HiddenUserServer), IsGroup: true}}})
	if b.catchUp.done != 1 {
		t.Fatalf("done = %d", b.catchUp.done)
	}
}

// TestHistoryProgress checks the stages of a history sync, and that older
// messages are waited for, across a restart too, until the phone gives up.
func TestHistoryProgress(t *testing.T) {
	const (
		boot   = waHistorySync.HistorySync_INITIAL_BOOTSTRAP
		recent = waHistorySync.HistorySync_RECENT
		full   = waHistorySync.HistorySync_FULL
	)
	b := testBackend(t)
	quiet := func(f func()) {
		b.history.mu.Lock()
		f()
		b.history.mu.Unlock()
	}
	want := func(want ...model.SyncEvent) {
		t.Helper()
		if got := polled[model.SyncEvent](b); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	}
	t.Cleanup(b.dropHistory)

	b.noteHistory(boot, 60)
	b.noteHistory(waHistorySync.HistorySync_PUSH_NAME, 100)
	want(model.SyncEvent{Stage: model.SyncChats, Percent: 60})
	quiet(b.historyQuiet) // the chats are done once chunks stop
	want(model.SyncEvent{Stage: model.SyncChats, Percent: 100, Done: true})

	b.noteHistory(recent, 40)
	b.noteHistory(recent, 30) // out of order
	b.noteHistory(boot, 100)  // late
	b.noteHistory(full, 4)
	want(model.SyncEvent{Stage: model.SyncOlder, Percent: 40}, model.SyncEvent{Stage: model.SyncOlder, Percent: 40},
		model.SyncEvent{Stage: model.SyncOlder, Percent: 4})
	quiet(b.historyQuiet)
	want(model.SyncEvent{Stage: model.SyncOlder, Percent: 4, Waiting: true})

	// The app closes and opens again.
	b.history.timer.Stop()
	b.history.stage, b.history.percent = model.SyncNone, 0
	b.resumeHistory()
	b.resumeHistory() // connected again: it's going on already
	want(model.SyncEvent{Stage: model.SyncOlder, Percent: 4, Waiting: true})
	b.noteHistory(full, 43)
	want(model.SyncEvent{Stage: model.SyncOlder, Percent: 43})
	quiet(b.historyGaveUp)
	want(model.SyncEvent{Stage: model.SyncOlder, Percent: 43, Done: true})
	b.resumeHistory() // it's over
	want()

	b.noteHistory(full, 100)
	quiet(b.historyQuiet)
	want(model.SyncEvent{Stage: model.SyncOlder, Percent: 99}, model.SyncEvent{Stage: model.SyncOlder, Percent: 100, Done: true})
}
