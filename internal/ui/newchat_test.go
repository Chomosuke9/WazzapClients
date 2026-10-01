package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

func TestPhoneDigits(t *testing.T) {
	for in, want := range map[string]string{
		"+62 812-3456-7890": "6281234567890",
		"(021) 555 0199":    "0215550199",
		"6281234":           "6281234",
		"12345":             "", // too short
		"Rina":              "", // a name
		"62 812 Rina":       "", // mixed
		"1+2345678":         "", // + only first
		"1234567890123456":  "", // too long
	} {
		if got := phoneDigits(in); got != want {
			t.Errorf("phoneDigits(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestNewGroupFlow goes from New chat through New group to the new group's
// chat, drawing frames as it goes.
func TestNewGroupFlow(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	now := time.Now()
	var ops op.Ops
	var r input.Router
	frame := func() {
		ops.Reset()
		u.Layout(layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1100, 700))})
		r.Frame(&ops)
		now = now.Add(100 * time.Millisecond)
	}
	frame()
	u.openNewChat()
	frame()
	if len(u.newChat.contacts) == 0 {
		t.Fatal("no contacts listed")
	}
	nc := &u.newChat
	nc.step = ncMembers
	nc.picked = []model.Contact{*nc.contacts[0], *nc.contacts[1]}
	frame()
	nc.step = ncGroup
	frame()
	u.createGroup()
	if nc.creating {
		t.Fatal("a group without a name was created")
	}
	nc.name.SetText("Weekend trip")
	u.createGroup()
	for range 5 {
		frame()
	}
	if nc.open() || u.selected == nil || u.selected.Name != "Weekend trip" || !u.selected.IsGroup {
		t.Fatalf("after creating, panel open %v, chat %+v", nc.open(), u.selected)
	}
	info := u.backend.Info(u.selected.ID)
	if info == nil || len(info.Members) != 3 {
		t.Fatalf("the group's members: %+v", info)
	}
	if nc.contacts != nil {
		t.Error("the contacts were kept after the panel closed")
	}
}

// TestSimilarGroupBack checks that Create a similar group starts with the
// members picked, and that Back from there closes the panel.
func TestSimilarGroupBack(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("work")
	u.openNewGroup([]model.Contact{{ID: "bima", Name: "Bima"}})
	if u.newChat.step != ncMembers || len(u.newChat.picked) != 1 {
		t.Fatalf("step %v with %d picked", u.newChat.step, len(u.newChat.picked))
	}
	u.newChatBack()
	if u.newChat.open() {
		t.Error("Back from a similar group didn't close the panel")
	}
	u.openNewChat()
	u.newChat.step = ncMembers
	u.newChatBack()
	if u.newChat.step != ncChat {
		t.Errorf("Back from New group went to %v, want New chat", u.newChat.step)
	}
}

// TestPhoneLookup types a number and expects its chat.
func TestPhoneLookup(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.openNewChat()
	u.newChat.search.SetText("+62 812 5550 0199")
	u.lookupPhone("6281255500199")
	u.applyEvents()
	if u.newChat.open() || u.selected == nil || u.selected.ID != "6281255500199@s.whatsapp.net" {
		t.Fatalf("panel open %v, chat %+v", u.newChat.open(), u.selected)
	}
}

// TestPhoneLookupStale checks that an answer for a number no longer typed
// doesn't open its chat.
func TestPhoneLookupStale(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.openNewChat()
	u.newChat.search.SetText("6281255500199")
	u.lookupPhone("6281255500199")
	u.newChat.search.SetText("Rina")
	u.applyEvents()
	if !u.newChat.open() || u.selected != nil {
		t.Fatalf("panel open %v, chat %+v", u.newChat.open(), u.selected)
	}
}
