package wa

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// TestLastPush checks that a mention of someone you haven't saved can use
// the push name stored with their messages, under their LID or phone JID.
func TestLastPush(t *testing.T) {
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
	for _, r := range []struct {
		id, sender, push string
		ts               int
	}{
		{"m1", "1@lid", "Old name", 1},
		{"m2", "628123@s.whatsapp.net", "New name", 2},
		{"m3", "1@lid", "", 3}, // sent without a push name
		{"m4", "2@lid", "Someone else", 4},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO wz_messages (chat, id, sender_jid, sender_push, ts) VALUES ('g', ?, ?, ?, ?)`,
			r.id, r.sender, r.push, r.ts); err != nil {
			t.Fatal(err)
		}
	}
	if got := s.lastPush(ctx, "1@lid", "628123@s.whatsapp.net"); got != "New name" {
		t.Errorf("lastPush = %q, want the newest of either JID", got)
	}
	if got := s.lastPush(ctx, "1@lid", ""); got != "Old name" {
		t.Errorf("lastPush by LID = %q", got)
	}
	if got := s.lastPush(ctx, "3@lid", ""); got != "" {
		t.Errorf("unknown sender: %q", got)
	}
}
