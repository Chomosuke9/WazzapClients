package wa

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/polymorfa/hypermeow/types"
)

// TestMemberLabel checks that a member you haven't saved goes by their push
// name, and by their number only without one.
func TestMemberLabel(t *testing.T) {
	j := types.NewJID("123", types.HiddenUserServer)
	for _, c := range []struct {
		n    contactNames
		want string
	}{
		{contactNames{saved: "Zaki", business: "Shop", push: "zak", phone: "+62 1"}, "Zaki"},
		{contactNames{business: "Shop", push: "zak", phone: "+62 1"}, "Shop"},
		{contactNames{push: "zak", phone: "+62 1"}, "~zak"},
		{contactNames{phone: "+62 1", redacted: "+62 •"}, "+62 1"},
		{contactNames{redacted: "+62 •"}, "+62 •"},
		{contactNames{}, "123"},
	} {
		if got := c.n.label(j); got != c.want {
			t.Errorf("%+v.label = %q, want %q", c.n, got, c.want)
		}
	}
}

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
