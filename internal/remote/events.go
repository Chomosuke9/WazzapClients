package remote

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
	"time"
)

// eventKeep is how many events the server keeps for watches catching up.
// One that falls further behind reloads everything.
const eventKeep = 256

// events is the last eventKeep events, numbered from 1. A cursor names the
// last one a watch has, as "<epoch>-<seq>": the epoch changes with every
// server, so a cursor from an earlier run of the app means "reload".
type events struct {
	mu      sync.Mutex
	epoch   string
	seq     int     // the last event's number
	buf     []Event // the events up to seq, oldest first
	changed chan struct{}
}

func newEvents() *events {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return &events{epoch: hex.EncodeToString(b[:]), changed: make(chan struct{})}
}

// add queues e and wakes the watches waiting for it.
func (q *events) add(e Event) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.seq++
	if len(q.buf) == eventKeep {
		copy(q.buf, q.buf[1:])
		q.buf = q.buf[:eventKeep-1]
	}
	q.buf = append(q.buf, e)
	close(q.changed)
	q.changed = make(chan struct{})
}

// cursor names the newest event.
func (q *events) cursor() string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.epoch + "-" + strconv.Itoa(q.seq)
}

// since returns the events after cursor and the cursor of the newest one,
// waiting up to wait for one to come. reset is set when the watch must
// reload everything: the cursor is from another run of the app, or too
// old.
func (q *events) since(ctx context.Context, cursor string, wait time.Duration) (evs []Event, next string, reset bool) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		q.mu.Lock()
		next = q.epoch + "-" + strconv.Itoa(q.seq)
		epoch, s, ok := strings.Cut(cursor, "-")
		n, err := strconv.Atoi(s)
		oldest := q.seq - len(q.buf) // the last one before buf
		if !ok || err != nil || epoch != q.epoch || n > q.seq || n < oldest {
			q.mu.Unlock()
			return nil, next, true
		}
		if n < q.seq {
			evs = append([]Event(nil), q.buf[n-oldest:]...)
			q.mu.Unlock()
			return evs, next, false
		}
		changed := q.changed
		q.mu.Unlock()
		select {
		case <-changed:
		case <-timer.C:
			return nil, next, false
		case <-ctx.Done():
			return nil, next, false
		}
	}
}
