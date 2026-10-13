package wa

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	waBinary "github.com/polymorfa/hypermeow/binary"
	"github.com/polymorfa/hypermeow/types"
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

// TestGroupedReceipts reads a group's grouped receipt as WhatsApp sends it:
// the message's ID in <participants message_id>, each person's step in
// their <user type>. hypermeow got both wrong, so it only acks the node.
func TestGroupedReceipts(t *testing.T) {
	b, err := Open(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx := b.ctx
	group := types.NewJID("120363408109130578", types.GroupServer)
	user := func(jid, typ string) waBinary.Node {
		j, _ := types.ParseJID(jid)
		return waBinary.Node{Tag: "user", Attrs: waBinary.Attrs{"jid": j, "t": "1791702535", "type": typ}}
	}
	node := &waBinary.Node{
		Tag:   "receipt",
		Attrs: waBinary.Attrs{"from": group, "id": "SAGRC67A27EC3AD246C225", "t": "1791702535"},
		Content: []waBinary.Node{{
			Tag:   "participants",
			Attrs: waBinary.Attrs{"message_id": "3EB0A279D0BE795F5508F1"},
			Content: []waBinary.Node{
				user("132598911267037:49@lid", "delivery"),
				user("132598911267037@lid", "delivery"),
				user("21492167393462:49@lid", "read"),
				user("112997049577684:2@lid", "inactive"),
			},
		}},
	}
	out, drop := b.groupedReceipts(ctx, node)
	if drop || out == nil {
		t.Fatalf("groupedReceipts = %v, %v; want the node without its users", out, drop)
	}
	if ps := out.GetChildrenByTag("participants"); len(ps) != 1 || len(ps[0].GetChildren()) != 0 {
		t.Fatalf("node passed on: %s", out)
	}
	b.work.Wait()

	got := map[string]personReceipt{}
	for _, r := range b.store.receipts(ctx, group.String(), "3EB0A279D0BE795F5508F1") {
		got[r.who] = r
	}
	want := map[string][2]int64{
		"132598911267037@lid": {1791702535, 0},
		"21492167393462@lid":  {1791702535, 1791702535},
	}
	if len(got) != len(want) {
		t.Errorf("receipts of %d people, want %d: %v", len(got), len(want), got)
	}
	for who, w := range want {
		if r := got[who]; [2]int64{r.delivered, r.read} != w {
			t.Errorf("%s: delivered, read = %d, %d; want %v", who, r.delivered, r.read, w)
		}
	}

	// A receipt from one person goes on to hypermeow as it came.
	one := &waBinary.Node{Tag: "receipt", Attrs: waBinary.Attrs{"from": group, "id": "x", "participant": "1@lid"}}
	if out, drop := b.groupedReceipts(ctx, one); out != nil || drop {
		t.Errorf("one person's receipt changed: %v, %v", out, drop)
	}
}
