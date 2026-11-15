package ui

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/mock"
)

// TestRestingStopsAnimations checks that nothing starts playing while the
// user is idle, that what played stops, and that their return asks for the
// frame that starts it again.
func TestRestingStopsAnimations(t *testing.T) {
	u := New(mock.New())
	woke := make(chan struct{}, 8)
	u.Start(func() {
		select {
		case woke <- struct{}{}:
		default:
		}
	})
	var idle atomic.Int64
	u.idle = func() time.Duration { return time.Duration(idle.Load()) }
	load := func() []byte { return nil }
	now := testNow()

	u.updateResting(now)
	if u.rest.on {
		t.Fatal("resting with the user here")
	}
	u.stickerFrame("s", 64, load)
	if len(u.players.m) != 1 {
		t.Fatalf("%d players with the user here, want 1", len(u.players.m))
	}
	u.players.endFrame()

	// Idle: the next look, a second later, rests.
	idle.Store(int64(idleAfter))
	now = now.Add(restCheck)
	u.updateResting(now)
	if !u.rest.on {
		t.Fatal("not resting after idleAfter without input")
	}
	if _, ok := u.stickerFrame("s", 64, load); ok {
		t.Error("a sticker plays while resting")
	}
	u.players.endFrame()
	if len(u.players.m) != 0 {
		t.Errorf("%d players while resting, want 0", len(u.players.m))
	}
	if _, ok := u.previewFrame("p", 64, load); ok || len(u.players.m) != 0 {
		t.Error("a preview started while resting")
	}

	// Back: the watcher asks for a frame, and that frame plays again.
	for len(woke) > 0 {
		<-woke
	}
	idle.Store(0)
	select {
	case <-woke:
	case <-time.After(5 * restPoll):
		t.Fatal("no frame asked for when the user came back")
	}
	u.updateResting(now.Add(time.Millisecond))
	if u.rest.on {
		t.Fatal("still resting with the user back")
	}
	u.stickerFrame("s", 64, load)
	if len(u.players.m) != 1 {
		t.Errorf("%d players after the user came back, want 1", len(u.players.m))
	}
	u.players.stopAll()
}
