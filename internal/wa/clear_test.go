package wa

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// TestClearUpTo checks that a clear or delete from another device keeps the
// messages after it. A full app state sync replays old ones, which used to
// empty the chat on every resync.
func TestClearUpTo(t *testing.T) {
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
	count := func(chat string) (msgs, chats int) {
		db.QueryRowContext(ctx, `SELECT count(*) FROM wz_messages WHERE chat = ?`, chat).Scan(&msgs)
		db.QueryRowContext(ctx, `SELECT count(*) FROM wz_chats WHERE jid = ?`, chat).Scan(&chats)
		return
	}
	for _, chat := range []string{"a", "b", "c"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO wz_chats (jid) VALUES (?)`, chat); err != nil {
			t.Fatal(err)
		}
		for ts := 1; ts <= 4; ts++ {
			if _, err := db.ExecContext(ctx, `INSERT INTO wz_messages (chat, id, ts) VALUES (?, ?, ?)`,
				chat, string(rune('0'+ts)), ts); err != nil {
				t.Fatal(err)
			}
		}
	}

	_ = s.clearChat(ctx, "a", 2)
	if m, c := count("a"); m != 2 || c != 1 {
		t.Errorf("clear up to 2: %d messages, %d chats; want 2, 1", m, c)
	}
	_ = s.deleteChat(ctx, "b", 2)
	if m, c := count("b"); m != 2 || c != 1 {
		t.Errorf("delete up to 2: %d messages, %d chats; want 2, 1", m, c)
	}
	_ = s.deleteChat(ctx, "c", 4)
	if m, c := count("c"); m != 0 || c != 0 {
		t.Errorf("delete up to 4: %d messages, %d chats; want 0, 0", m, c)
	}
	_ = s.clearChat(ctx, "a", 0)
	if m, _ := count("a"); m != 0 {
		t.Errorf("clear all: %d messages left", m)
	}
}
