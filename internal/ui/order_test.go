package ui

import (
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestMessageOrder checks that messages arriving in the same second keep
// their arrival order, as the store keeps them, though yours carry
// milliseconds and others' don't, and that a sent message moves to the
// time the server gives it.
func TestMessageOrder(t *testing.T) {
	st := newSlashTest(t, "work")
	u := st.u
	sec := testNow().Add(time.Hour).Truncate(time.Second)
	mine := &model.Message{ID: "order-mine", ChatID: "work", FromMe: true, Text: "a", Time: sec.Add(700 * time.Millisecond)}
	theirs := &model.Message{ID: "order-theirs", ChatID: "work", Text: "b", Time: sec}
	u.upsertMessage(mine)
	u.upsertMessage(theirs)
	ids := func() (string, string) {
		n := len(u.msgs)
		return u.msgs[n-2].ID, u.msgs[n-1].ID
	}
	if a, b := ids(); a != mine.ID || b != theirs.ID {
		t.Fatalf("same second: last two %s, %s; want yours, then theirs", a, b)
	}

	// The server says yours came a second later.
	moved := *mine
	moved.Time = sec.Add(time.Second)
	u.upsertMessage(&moved)
	if a, b := ids(); a != theirs.ID || b != mine.ID {
		t.Fatalf("after the server's time: last two %s, %s; want theirs, then yours", a, b)
	}
}
