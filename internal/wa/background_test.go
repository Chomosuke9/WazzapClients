package wa

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// An account in the background that isn't linked (any more) doesn't link
// again, which only the window can show: it reports the QR codes expired.
func TestBackgroundDoesntPair(t *testing.T) {
	b, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	b.SetBackground(true)
	wake := make(chan struct{}, 1)
	b.Start(func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	})
	defer b.Close()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-wake:
		case <-deadline:
			t.Fatal("no StateQRExpired")
		}
		for _, ev := range b.Poll() {
			if e, ok := ev.(model.ConnEvent); ok {
				if e.State != model.StateQRExpired {
					t.Fatalf("state %v, want StateQRExpired", e.State)
				}
				return
			}
		}
	}
}

// Close waits for what runs after a connect, which reads the database,
// and nothing starts once it closes.
func TestCloseWaitsForWork(t *testing.T) {
	b, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	var done, late atomic.Bool
	b.goWork(func() {
		<-b.ctx.Done()
		time.Sleep(50 * time.Millisecond)
		_ = b.store.meta(b.ctx, appStateResyncKey)
		done.Store(true)
	})
	b.Close()
	if !done.Load() {
		t.Error("Close returned before the work finished")
	}
	b.goWork(func() { late.Store(true) })
	time.Sleep(20 * time.Millisecond)
	if late.Load() {
		t.Error("work started after Close")
	}
}
