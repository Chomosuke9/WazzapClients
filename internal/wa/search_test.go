package wa

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestSearchMessages checks that a chat's search finds text in any case,
// beyond ASCII, newest first, treats LIKE wildcards literally, and skips other chats and
// deleted messages.
func TestSearchMessages(t *testing.T) {
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
	for _, m := range []struct {
		chat, id string
		ts       int
		kind     model.Kind
		text     string
	}{
		{"a", "1", 1, model.KindText, "Lunch at noon?"},
		{"a", "2", 2, model.KindText, "50% off lunch"},
		{"a", "3", 3, model.KindDeleted, "lunch"},
		{"a", "4", 4, model.KindText, "LUNCH again"},
		{"a", "5", 5, model.KindText, "500 off"},
		{"b", "6", 6, model.KindText, "lunch"},
		{"a", "7", 7, model.KindText, "ÉTÉ à Paris"},
		{"a", "8", 8, model.KindDeleted, "été"},
		{"a", "9", 9, model.KindText, "un été"},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO wz_messages (chat, id, ts, kind, text) VALUES (?, ?, ?, ?, ?)`,
			m.chat, m.id, m.ts, int(m.kind), m.text); err != nil {
			t.Fatal(err)
		}
	}
	ids := func(q string, limit int) (out []string) {
		raw, err := s.searchMessages(ctx, "a", q, limit)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range raw {
			out = append(out, r.ID)
		}
		return out
	}
	for _, c := range []struct {
		q     string
		limit int
		want  string
	}{
		{"lunch", 10, "4 2 1"},
		{"lunch", 2, "4 2"},
		{"0%", 10, "2"},
		{"5_0", 10, ""},
		{"été", 10, "9 7"},
		{"été", 1, "9"},
		{"À p", 10, "7"},
	} {
		got := ""
		for i, id := range ids(c.q, c.limit) {
			if i > 0 {
				got += " "
			}
			got += id
		}
		if got != c.want {
			t.Errorf("search %q (limit %d) = %q, want %q", c.q, c.limit, got, c.want)
		}
	}
}
