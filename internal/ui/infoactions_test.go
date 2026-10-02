package ui

import (
	"image"
	"slices"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
)

// TestInfoActions clicks the group info panel's actions and checks that
// each one does what it says.
func TestInfoActions(t *testing.T) {
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	u.SelectID("family")
	now := testNow()
	var ops op.Ops
	var r input.Router
	frame := func() {
		ops.Reset()
		u.Layout(layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1400, 900))})
		r.Frame(&ops)
		now = now.Add(10 * time.Millisecond)
	}
	click := func(key string) {
		u.btn(key).Click()
		frame()
		frame()
	}
	u.openInfo("family")
	for range 30 {
		frame()
	}
	// The panel lists its actions only once scrolled to them; they read
	// their clicks while drawn, so show them all.
	u.info.list.Position.First = 1 << 20
	frame()
	frame()

	click("info:fav")
	if !u.chatByID("family").Favorite {
		t.Error("Add to favourites didn't add the group")
	}

	click("info:list")
	if u.ctx.kind != ctxLists || !u.ctx.isOpen() {
		t.Fatalf("Add to list opened menu %v", u.ctx.kind)
	}
	click("menu:l2")
	if !slices.Contains(b.Lists()[1].Chats, "family") {
		t.Error("ticking Work didn't add the group to it")
	}
	if !u.ctx.isOpen() {
		t.Error("the lists menu closed after one tick")
	}
	u.closeMenu()
	for range 30 {
		frame()
	}

	u.info.list.Position.First = 0
	frame()
	click("info:notif")
	if u.ctx.kind != ctxMute {
		t.Fatalf("Notification settings opened menu %v", u.ctx.kind)
	}
	click("menu:8h")
	if c := u.chatByID("family"); !c.Muted || c.MuteUntil.Sub(testNow()) < 7*time.Hour {
		t.Errorf("Mute for 8 hours left muted=%v until %v", c.Muted, c.MuteUntil)
	}
	now = now.Add(9 * time.Hour)
	frame()
	if c := u.chatByID("family"); c.Muted {
		t.Error("the mute outlasted its 8 hours")
	}
	for range 30 {
		frame()
	}

	u.info.list.Position.First = 1 << 20
	frame()
	frame()
	click("info:exit")
	if u.dialog.kind != dialogConfirm || u.dialog.buttons[0].label != "Exit group" {
		t.Errorf("Exit group didn't ask first: %+v", u.dialog)
	}
}
