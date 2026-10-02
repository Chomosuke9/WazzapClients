package wa

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// TestPutReceipt checks that each person's receipts keep the earliest time
// of each step, whatever order they come in, and that reading implies
// delivery and playing implies reading.
func TestPutReceipt(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &msgStore{db: db}
	if err := s.init(ctx); err != nil {
		t.Fatal(err)
	}
	put := func(r personReceipt) {
		t.Helper()
		r.chat, r.id = "g", "m"
		if err := s.putReceipt(ctx, db, r); err != nil {
			t.Fatal(err)
		}
	}
	put(personReceipt{who: "a", read: 50})      // read before the delivery receipt came
	put(personReceipt{who: "a", delivered: 40}) // the earlier delivery
	put(personReceipt{who: "a", delivered: 45}) // a repeat, later
	put(personReceipt{who: "b", played: 70})
	put(personReceipt{who: "c", delivered: 30})

	got := map[string]personReceipt{}
	for _, r := range s.receipts(ctx, "g", "m") {
		got[r.who] = r
	}
	want := map[string][3]int64{"a": {40, 50, 0}, "b": {70, 70, 70}, "c": {30, 0, 0}}
	for who, w := range want {
		r := got[who]
		if [3]int64{r.delivered, r.read, r.played} != w {
			t.Errorf("%s: delivered, read, played = %d, %d, %d; want %v", who, r.delivered, r.read, r.played, w)
		}
	}

	// Deleting the message takes its receipts with it.
	if _, err := db.ExecContext(ctx, `INSERT INTO wz_messages (chat, id, ts) VALUES ('g', 'm', 1)`); err != nil {
		t.Fatal(err)
	}
	if err := s.deleteMessage(ctx, "g", "m"); err != nil {
		t.Fatal(err)
	}
	if n := len(s.receipts(ctx, "g", "m")); n != 0 {
		t.Errorf("%d receipts left after deleting the message", n)
	}
}
