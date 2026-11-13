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

// TestStickerReactions checks that the reaction picker has a Sticker tab
// once its extra feature is on, and that the list of who reacted takes yours back.
func TestStickerReactions(t *testing.T) {
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	u.SelectID("rina")
	u.applyEvents()
	now := testNow()
	var ops op.Ops
	var r input.Router
	frame := func() {
		for range 20 {
			ops.Reset()
			u.Layout(layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
				Constraints: layout.Exact(image.Pt(1100, 700))})
			r.Frame(&ops)
			now = now.Add(50 * time.Millisecond)
		}
		u.applyEvents()
	}
	frame()
	target := u.msgs[len(u.msgs)-1]
	msg := func() *model.Message {
		t.Helper()
		for _, m := range u.msgs {
			if m.ID == target.ID {
				return m
			}
		}
		t.Fatal("the message is gone")
		return nil
	}
	// The Sticker tab is an extra feature, off until it's turned on.
	u.openPicker(pickReaction, msg())
	if u.stickerReact || u.stickerReacts() {
		t.Fatal("the reaction picker has a Sticker tab by default")
	}
	u.closePicker()
	u.ShowPage("extras")
	settingRowByKey(t, u, prefStickerReact).run()
	u.setPage(pageChats)
	frame()
	if !u.stickerReact || u.backend.Pref(prefStickerReact) != "on" {
		t.Fatalf("after its switch: on %v, pref %q", u.stickerReact, u.backend.Pref(prefStickerReact))
	}

	// The reaction picker switches to its Sticker tab.
	u.openPicker(pickReaction, msg())
	frame()
	if !u.stickerReacts() || u.picker.tab != tabEmoji {
		t.Fatalf("the reaction picker: sticker tab %v, open tab %v", u.stickerReacts(), u.picker.tab)
	}
	u.btn("ptab:" + itoa(int(tabSticker)+1)).Click()
	frame()
	if u.picker.tab != tabSticker {
		t.Fatalf("tab after clicking Sticker = %v", u.picker.tab)
	}
	if u.picker.composerTab == tabSticker {
		t.Error("a reaction's tab became the composer's")
	}
	u.closePicker()
	frame()
	u.openPicker(pickReaction, &model.Message{ChatID: "techdaily@newsletter", ID: "x"})
	if u.stickerReacts() {
		t.Error("a channel's update takes sticker reactions")
	}
	u.closePicker()
	frame()
	if u.announcementsOf(u.chatByID("work-ann")) == nil {
		t.Fatal("work-ann isn't a community's announcements")
	}
	u.openPicker(pickReaction, &model.Message{ChatID: "work-ann", ID: "x"})
	if u.stickerReacts() || u.takesStickers(&model.Message{ChatID: "work-ann"}) || !u.takesStickers(msg()) {
		t.Error("a community's announcements take sticker reactions")
	}
	u.closePicker()
	frame()
	u.openPicker(pickReaction, &model.Message{ChatID: "rina", ID: "x", Kind: model.KindSticker, Media: model.MediaSticker})
	if u.stickerReacts() {
		t.Error("a sticker takes sticker reactions")
	}
	u.closePicker()
	frame()

	// A sticker of the picker reacts, and a second one replaces it.
	for i := range 2 {
		u.backend.ReactSticker(msg(), &model.Message{ChatID: "stickers", ID: "s" + itoa(i), Kind: model.KindSticker, Media: model.MediaSticker})
		frame()
	}
	st := msg().Stickers
	if len(st) != 1 || !st[0].Me {
		t.Fatalf("sticker reactions = %+v, want yours alone", st)
	}
	if my, ok := msg().MySticker(); !ok || my.ID != st[0].ID {
		t.Errorf("MySticker = %+v, %v", my, ok)
	}
	// Clicking it lists who reacted, on the Stickers tab; your row takes
	// it back, and the empty list closes.
	u.btn("stickerreact:" + target.ChatID + "/" + st[0].ID).Click()
	frame()
	if !u.reacts.isOpen() || u.reacts.tab != stickerTab || len(u.reacts.list) != 1 || u.reacts.list[0].sticker == nil {
		t.Fatalf("who reacted: open %v, tab %q, %+v", u.reacts.isOpen(), u.reacts.tab, u.reacts.list)
	}
	u.btn("reactor:mysticker/" + target.ID).Click()
	frame()
	frame()
	if n := len(msg().Stickers); n != 0 {
		t.Fatalf("%d sticker reactions after taking yours back", n)
	}
	if u.reacts.isOpen() {
		t.Error("the empty list stayed open")
	}
}
