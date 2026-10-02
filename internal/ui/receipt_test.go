package ui

import (
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestFailedReceipt checks that a send that failed swaps a pending
// message's clock for the failed state, never undoes a later receipt, and
// gives way to a receipt that arrives after all.
func TestFailedReceipt(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("rina")
	pending := &model.Message{ID: "p", ChatID: "rina", FromMe: true, Receipt: model.Pending, Time: testNow()}
	sent := &model.Message{ID: "s", ChatID: "rina", FromMe: true, Receipt: model.Sent, Time: testNow()}
	u.msgs = []*model.Message{pending, sent}

	u.applyReceipt(model.ReceiptEvent{ChatID: "rina", IDs: []string{"p", "s"}, Receipt: model.Failed})
	if pending.Receipt != model.Failed {
		t.Errorf("pending message: receipt %d, want failed", pending.Receipt)
	}
	if sent.Receipt != model.Sent {
		t.Errorf("sent message: receipt %d, want it kept", sent.Receipt)
	}
	if ic, _ := receiptIcon(pending.Receipt, u.pal, true); ic != icFailed {
		t.Error("failed message doesn't show the failed icon")
	}

	u.applyReceipt(model.ReceiptEvent{ChatID: "rina", IDs: []string{"p"}, Receipt: model.Delivered})
	if pending.Receipt != model.Delivered {
		t.Errorf("late receipt: receipt %d, want delivered", pending.Receipt)
	}
}
