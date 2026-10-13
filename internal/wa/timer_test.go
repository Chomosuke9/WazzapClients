package wa

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

func testStore(t *testing.T) (*msgStore, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := &msgStore{db: db}
	if err := s.init(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, db
}

// TestChatTimer checks that a chat's disappearing timer reaches the chat
// list, and that a history sync's older copy doesn't undo a timer set
// since.
func TestChatTimer(t *testing.T) {
	ctx := context.Background()
	s, db := testStore(t)
	for _, j := range []string{"a@lid", "b@lid"} {
		if err := s.ensureChat(ctx, db, j, false, j); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.setField(ctx, "a@lid", "ephemeral", int64(86400)); err != nil {
		t.Fatal(err)
	}
	// The history, taken when the device linked, says off for a and a
	// week for b.
	_ = s.setMeta(ctx, db, "a@lid", chatMeta{lastTS: 5})
	_ = s.setMeta(ctx, db, "b@lid", chatMeta{lastTS: 5, ephemeral: 604800})
	for j, want := range map[string]uint32{"a@lid": 86400, "b@lid": 604800} {
		c, ok := s.chat(ctx, j)
		if !ok || c.Disappearing != want {
			t.Errorf("%s: timer %d, want %d", j, c.Disappearing, want)
		}
		if got, _ := s.timer(ctx, j); got != want {
			t.Errorf("%s: stored timer %d, want %d", j, got, want)
		}
	}
}

// TestSearchAllMessages checks that the chat list's search finds messages
// of every chat but channels, newest first whatever order they were
// stored in.
func TestSearchAllMessages(t *testing.T) {
	ctx := context.Background()
	s, db := testStore(t)
	for _, m := range []struct {
		chat, id string
		ts       int
		kind     model.Kind
		text     string
	}{
		{"a", "1", 5, model.KindText, "Lunch?"},
		{"b", "2", 9, model.KindText, "lunch is late"},
		{"c@newsletter", "3", 10, model.KindText, "lunch deals"},
		{"a", "4", 1, model.KindText, "lunch, a while ago"}, // came later, with the history
		{"b", "5", 7, model.KindDeleted, "lunch"},
		{"b", "6", 8, model.KindText, "dinner"},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO wz_messages (chat, id, ts, kind, text) VALUES (?, ?, ?, ?, ?)`,
			m.chat, m.id, m.ts, int(m.kind), m.text); err != nil {
			t.Fatal(err)
		}
	}
	for limit, want := range map[int]string{10: "b/2 a/1 a/4", 2: "b/2 a/1"} {
		raw, err := s.searchAllMessages(ctx, model.SearchKey("LUNCH"), limit)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, r := range raw {
			got = append(got, r.ChatID+"/"+r.ID)
		}
		if strings.Join(got, " ") != want {
			t.Errorf("limit %d: found %q, want %q", limit, strings.Join(got, " "), want)
		}
	}
}

// TestNextList checks that a new list gets the next label number and the
// last place.
func TestNextList(t *testing.T) {
	ctx := context.Background()
	s, _ := testStore(t)
	id, ord, err := s.nextList(ctx)
	if err != nil || id != "1" || ord != 1 {
		t.Fatalf("empty store: list %q at %d (%v)", id, ord, err)
	}
	_ = s.putList(ctx, "5", "Unread", false, false, 2)
	_ = s.putList(ctx, "12", "Work", true, false, 4)
	if id, ord, _ := s.nextList(ctx); id != "13" || ord != 5 {
		t.Errorf("next list %q at %d, want 13 at 5", id, ord)
	}
}
