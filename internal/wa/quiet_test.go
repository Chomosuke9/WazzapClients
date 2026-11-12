package wa

import (
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waWeb"
	"github.com/polymorfa/hypermeow/types"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestSecurityCodeQuiet checks that a contact's security code changing
// shows in the chat but isn't its latest activity: the list keeps the
// chat's last real message and time, and a chat with nothing else stays
// out of the list.
func TestSecurityCodeQuiet(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	ana := types.NewJID("111", types.HiddenUserServer)
	bob := types.NewJID("222", types.HiddenUserServer)
	if err := b.store.ensureChat(ctx, b.db, ana.String(), false, "Ana"); err != nil {
		t.Fatal(err)
	}
	hi := &model.Message{ID: "hi", ChatID: ana.String(), Kind: model.KindText, Text: "hi", Time: time.Unix(1000, 0)}
	if err := b.store.putMessage(ctx, b.db, storedMsg{Message: hi}); err != nil {
		t.Fatal(err)
	}
	change := func(j types.JID, at int64) {
		b.putSystem(ctx, systemMsg(j.String(), "sec-"+j.User, time.Unix(at, 0), j, false,
			waWeb.WebMessageInfo_E2E_IDENTITY_CHANGED, j.String()))
	}
	change(ana, 5000)
	change(bob, 5000) // Bob has no chat but this

	if got := b.Messages(ana.String(), 10); len(got) != 2 || got[1].Notice != model.NoticeSecurity {
		t.Fatalf("Ana's chat shows %+v, want hi then the security code change", got)
	}
	var listedAna bool
	for _, c := range b.Chats() {
		switch c.ID {
		case bob.String():
			t.Errorf("a chat with only a security code change is listed: %+v", c)
		case ana.String():
			listedAna = true
			if c.Last == nil || c.Last.ID != "hi" || c.Time.Unix() != 1000 {
				t.Errorf("Ana's chat = last %+v at %v, want hi at 1000", c.Last, c.Time.Unix())
			}
		}
	}
	if !listedAna {
		t.Errorf("Ana's chat isn't listed")
	}
	if b.store.listed(ctx, bob.String()) {
		t.Errorf("Bob's chat counts as listed")
	}
}

// TestQuietOldStubs checks that security code changes stored before the
// quiet column existed are marked quiet once, and that a chat they moved
// up goes back to its last real message, or out of the list.
func TestQuietOldStubs(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	ana, bob := "111@lid", "222@lid"
	for _, c := range []string{ana, bob} {
		if err := b.store.ensureChat(ctx, b.db, c, false, ""); err != nil {
			t.Fatal(err)
		}
	}
	hi := &model.Message{ID: "hi", ChatID: ana, Kind: model.KindText, Text: "hi", Time: time.Unix(1000, 0)}
	if err := b.store.putMessage(ctx, b.db, storedMsg{Message: hi}); err != nil {
		t.Fatal(err)
	}
	// As an older version stored them: not quiet, and moving the chat up.
	for _, c := range []string{ana, bob} {
		j, _ := types.ParseJID(c)
		m := systemMsg(c, "sec", time.Unix(5000, 0), j, false, waWeb.WebMessageInfo_E2E_IDENTITY_CHANGED, c)
		if _, err := b.db.ExecContext(ctx, `INSERT INTO wz_messages (chat, id, ts, kind, raw_payload) VALUES (?, ?, ?, ?, ?)`,
			c, m.ID, 5000, int(model.KindSystem), m.rawPayload); err != nil {
			t.Fatal(err)
		}
		if _, err := b.db.ExecContext(ctx, `UPDATE wz_chats SET last_ts = 5000 WHERE jid = ?`, c); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.db.ExecContext(ctx, `DELETE FROM wz_meta WHERE key = 'quiet_stubs_marked'`); err != nil {
		t.Fatal(err)
	}
	if err := b.store.quietOldStubs(ctx); err != nil {
		t.Fatal(err)
	}
	chats := b.Chats()
	if len(chats) != 1 || chats[0].ID != ana || chats[0].Time.Unix() != 1000 || chats[0].Last.ID != "hi" {
		for _, c := range chats {
			t.Logf("chat %s at %d, last %+v", c.ID, c.Time.Unix(), c.Last)
		}
		t.Fatalf("after marking old changes quiet, want only Ana's chat at 1000 with hi")
	}
}
