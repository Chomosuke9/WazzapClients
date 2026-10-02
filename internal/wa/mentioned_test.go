package wa

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// TestMentionedFlag checks that a chat shows "@" while an unread message is
// for you, and that the flag starts over once the chat is read.
func TestMentionedFlag(t *testing.T) {
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
	if _, err := db.ExecContext(ctx, `INSERT INTO wz_chats (jid, is_group, last_ts) VALUES ('g@g.us', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	check := func(step string, unread int, mentioned bool) {
		t.Helper()
		c, ok := s.chat(ctx, "g@g.us")
		if !ok {
			t.Fatal("chat not found")
		}
		if c.Unread != unread || c.Mentioned != mentioned {
			t.Errorf("%s: unread %d, mentioned %v; want %d, %v", step, c.Unread, c.Mentioned, unread, mentioned)
		}
	}
	_ = s.addUnread(ctx, "g@g.us", false)
	check("plain message", 1, false)
	_ = s.addUnread(ctx, "g@g.us", true)
	check("mention", 2, true)
	_ = s.addUnread(ctx, "g@g.us", false)
	check("plain after mention", 3, true)
	_ = s.setField(ctx, "g@g.us", "unread", 0)
	check("read", 0, false)
	_ = s.addUnread(ctx, "g@g.us", false)
	check("plain after read", 1, false)
	_ = s.setField(ctx, "g@g.us", "unread", -1) // marked as unread
	_ = s.addUnread(ctx, "g@g.us", false)
	check("plain after marked unread", 1, false)
}
