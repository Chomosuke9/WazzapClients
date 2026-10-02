package wa

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestFailedSend checks that a message that couldn't be sent stops being
// pending, both when its send fails and when the app closed before it went
// out, and that a receipt arriving after all still lifts it.
func TestFailedSend(t *testing.T) {
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
	put := func(id string, fromMe bool, r model.Receipt) {
		m := &model.Message{ChatID: "a", ID: id, FromMe: fromMe, Receipt: r}
		if err := s.putMessage(ctx, db, storedMsg{Message: m}); err != nil {
			t.Fatal(err)
		}
	}
	receipt := func(id string) model.Receipt {
		var r int
		if err := db.QueryRowContext(ctx, `SELECT receipt FROM wz_messages WHERE chat = 'a' AND id = ?`, id).Scan(&r); err != nil {
			t.Fatal(err)
		}
		return model.Receipt(r)
	}
	put("fail", true, model.Pending)
	put("sent", true, model.Sent)
	put("stale", true, model.Pending)
	put("in", false, model.Pending)

	_ = s.setFailed(ctx, "a", "fail")
	_ = s.setFailed(ctx, "a", "sent")
	if r := receipt("fail"); r != model.Failed {
		t.Errorf("failed send: receipt %d, want %d", r, model.Failed)
	}
	if r := receipt("sent"); r != model.Sent {
		t.Errorf("sent message marked failed: receipt %d", r)
	}

	// Opening the store again fails what an earlier run left pending.
	if err := s.init(ctx); err != nil {
		t.Fatal(err)
	}
	if r := receipt("stale"); r != model.Failed {
		t.Errorf("stale pending message: receipt %d, want %d", r, model.Failed)
	}
	if r := receipt("in"); r != model.Pending {
		t.Errorf("incoming message: receipt %d, want it untouched", r)
	}

	_ = s.setReceipt(ctx, "a", []string{"fail"}, model.Delivered)
	if r := receipt("fail"); r != model.Delivered {
		t.Errorf("late receipt: receipt %d, want %d", r, model.Delivered)
	}
}
