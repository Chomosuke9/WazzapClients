package ui

import (
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
)

// TestListSplit checks the list column keeps a dragged width within its
// limits, and that its width and being hidden stick.
func TestListSplit(t *testing.T) {
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1}}
	if w := u.listWidth(gtx, 1500); w != 430 {
		t.Errorf("default width = %d, want 430", w)
	}
	u.split.w = 100
	if w := u.listWidth(gtx, 1500); w != int(listMinW) {
		t.Errorf("width dragged to 100 = %d, want the least, %v", w, listMinW)
	}
	u.split.w = 1400
	if w := u.listWidth(gtx, 1500); w != 1500-int(convMinW) {
		t.Errorf("width dragged to 1400 = %d, want room for the chat", w)
	}
	u.split.w = 640
	u.saveSplit()
	u.setListHidden(true)

	u = New(b)
	if u.split.w != 640 || !u.split.hidden {
		t.Errorf("a new window's list is %v wide, hidden %v; want 640, hidden", u.split.w, u.split.hidden)
	}
	if v := u.listShown(gtx); v != 0 {
		t.Errorf("a hidden list shows %v", v)
	}
	// Opening the New chat panel, which lives in the list, shows it.
	u.newChat.step = ncChat
	if u.listShown(gtx); u.split.hidden {
		t.Error("the New chat panel left the list hidden")
	}
}
