package ui

import (
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestMsgInfo checks that Message info opens for your messages only, in
// place of the info panel, follows receipts, and closes with Esc.
func TestMsgInfo(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("work")
	var mine, theirs *model.Message
	for _, m := range u.msgs {
		if m.FromMe && m.Kind != model.KindDeleted && m.Receipt > model.Pending {
			mine = m
		} else if !m.FromMe {
			theirs = m
		}
	}
	if mine == nil || theirs == nil {
		t.Fatal("demo group has no messages of both kinds")
	}
	c := u.chatByID("work")
	has := func(m *model.Message) bool {
		for _, it := range u.messageMenuItems(c, m) {
			if it.key == "info" {
				return true
			}
		}
		return false
	}
	if !has(mine) || has(theirs) {
		t.Errorf("Message info in the menu: yours %v, theirs %v; want true, false", has(mine), has(theirs))
	}

	u.info.open = true
	u.openMsgInfo(mine)
	if u.info.open || !u.msgInfo.open || u.msgInfo.data == nil {
		t.Fatal("Message info didn't take the info panel's place")
	}
	if got := u.msgInfo.data.Members; got < 2 {
		t.Errorf("group message went to %d members", got)
	}
	u.msgInfo.data = nil
	u.msgInfoReceipt(model.ReceiptEvent{ChatID: "work", IDs: []string{mine.ID}, Receipt: model.Read})
	if u.msgInfo.data == nil {
		t.Error("a receipt for the message didn't reload its info")
	}
	if len(u.msgInfoRows()) < 3 {
		t.Error("Message info lists nothing")
	}
	u.escape()
	if u.msgInfo.open {
		t.Error("Esc didn't close Message info")
	}
}
