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

	// A sticker of the picker reacts, and a second one adds to it.
	react := func(i int) {
		u.backend.ReactSticker(msg(), &model.Message{ChatID: "stickers", ID: "s" + itoa(i), Kind: model.KindSticker, Media: model.MediaSticker})
		frame()
	}
	for i := range 2 {
		react(i)
	}
	st := msg().Stickers
	if len(st) != 2 || !st[0].Me || !st[1].Me {
		t.Fatalf("sticker reactions = %+v, want two of yours", st)
	}
	// Clicking one lists who reacted, on the Stickers tab; each of your
	// rows takes its sticker back, and the empty list closes.
	u.btn("stickerreact:" + target.ChatID + "/" + st[0].ID).Click()
	frame()
	if !u.reacts.isOpen() || u.reacts.tab != stickerTab || len(u.reacts.list) != 2 || u.reacts.list[0].sticker == nil {
		t.Fatalf("who reacted: open %v, tab %q, %+v", u.reacts.isOpen(), u.reacts.tab, u.reacts.list)
	}
	u.btn("reactor:mysticker/" + target.ID + "/" + st[1].ID).Click()
	frame()
	frame()
	if left := msg().Stickers; len(left) != 1 || left[0].ID != st[0].ID {
		t.Fatalf("sticker reactions after taking the newest back = %+v", left)
	}
	u.btn("reactor:mysticker/" + target.ID + "/" + st[0].ID).Click()
	frame()
	frame()
	if n := len(msg().Stickers); n != 0 {
		t.Fatalf("%d sticker reactions after taking yours back", n)
	}
	if u.reacts.isOpen() {
		t.Error("the empty list stayed open")
	}
}

// stickerRing makes a row of a few stickers along the bubble's bottom,
// and takes more around the bubble, squeezed, up to the most that fit.
func TestStickerRing(t *testing.T) {
	const sz, gap, overlap, start = 52, 1, 10, -4
	bubble := image.Pt(400, 120)
	row := stickerRing(3, sz, gap, overlap, start, bubble)
	for i, p := range row {
		if want := image.Pt(start+i*(sz+gap), bubble.Y-overlap); p != want {
			t.Errorf("row: sticker %d at %v, want %v", i, p, want)
		}
	}

	at := stickerRing(model.StickerReactsMax, sz, gap, overlap, start, bubble)
	if len(at) != model.StickerReactsMax {
		t.Fatalf("%d places for %d stickers", len(at), model.StickerReactsMax)
	}
	right, top := bubble.X-overlap, overlap-sz
	var side, above int
	for i, p := range at {
		switch {
		case p.Y == top:
			above++
		case p.X == right && p.Y < bubble.Y-overlap:
			side++
		case p.Y != bubble.Y-overlap:
			t.Errorf("sticker %d at %v, off the ring", i, p)
		}
		if p.X < start || p.X > right {
			t.Errorf("sticker %d at %v, past the ring's ends", i, p)
		}
		if i > 0 {
			if d := p.Sub(at[i-1]); float32(max(d.X, -d.X, d.Y, -d.Y)) < sz*stickerSqueezeMin-1 {
				t.Errorf("stickers %d and %d %v apart", i-1, i, d)
			}
		}
	}
	if side == 0 || above == 0 {
		t.Errorf("%d stickers up the side and %d along the top, want some of each", side, above)
	}
	if at[0] != image.Pt(start, bubble.Y-overlap) {
		t.Errorf("the newest at %v, want the start", at[0])
	}

	if n := len(stickerRing(500, sz, gap, overlap, start, bubble)); n >= 500 || n < model.StickerReactsMax {
		t.Errorf("%d places for 500 stickers, want the most that fit", n)
	}
}

// Each person keeps their newest model.StickerReactsMax sticker
// reactions: one more takes your oldest back.
func TestStickerReactsMax(t *testing.T) {
	b := mock.New()
	m := b.Messages("rina", 1)[0]
	for i := range model.StickerReactsMax + 1 {
		b.ReactSticker(m, &model.Message{ChatID: "stickers", ID: "s" + itoa(i), Kind: model.KindSticker, Media: model.MediaSticker})
	}
	st := b.Messages("rina", 1)[0].Stickers
	if len(st) != model.StickerReactsMax || st[0].ID == m.ID+"-react-1" {
		t.Fatalf("%d sticker reactions, the oldest %q", len(st), st[0].ID)
	}
}

// Around a small bubble the stickers squeeze only so far, and the oldest
// don't show, as in a phone's screen recordings: 12 of 18 around a
// two-line bubble, 6 around a one-word one, and 4 after its emojis' pill.
func TestStickerRingFull(t *testing.T) {
	sz, gap, overlap, start := stickerReactDp, stickerReactGapDp, stickerReactOverlapDp, stickerReactStartDp
	for _, c := range []struct {
		n, start int
		bubble   image.Point
		want     int
	}{
		{18, start, image.Pt(125, 95), 12},
		{6, start, image.Pt(84, 34), 6},
		{7, start, image.Pt(84, 34), 6},
		{6, 30, image.Pt(84, 34), 4},
	} {
		if got := len(stickerRing(c.n, sz, gap, overlap, c.start, c.bubble)); got != c.want {
			t.Errorf("%d stickers from %d around %v: %d show, want %d", c.n, c.start, c.bubble, got, c.want)
		}
	}
}

// Reacting with a sticker closes the reaction picker, or leaves it open
// for another with Keep the picker open on; an emoji closes it either way.
func TestStickerReactKeepPicker(t *testing.T) {
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	u.SelectID("rina")
	u.applyEvents()
	now := testNow()
	var ops op.Ops
	var r input.Router
	frame := func() {
		for range 5 {
			ops.Reset()
			u.Layout(layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
				Constraints: layout.Exact(image.Pt(1100, 700))})
			r.Frame(&ops)
			now = now.Add(50 * time.Millisecond)
		}
		u.applyEvents()
	}
	frame()
	b.SetPref(prefStickerReact, "on")
	u.stickerReact = true
	target := u.msgs[len(u.msgs)-1]
	stickers := func() int {
		for _, m := range u.msgs {
			if m.ID == target.ID {
				return len(m.Stickers)
			}
		}
		return -1
	}
	react := func() {
		t.Helper()
		u.openPicker(pickReaction, target)
		u.picker.anim.snap(true)
		frame()
		u.btn("ptab:" + itoa(int(tabSticker)+1)).Click()
		frame()
		// The demo has no stickers of its own to pick.
		set := u.picker.stickerSet
		u.picker.stickers[set] = []*model.Message{{ChatID: "stickers", ID: "s1", Kind: model.KindSticker, Media: model.MediaSticker}}
		u.picker.stickersOK[set] = true
		list := u.stickerList(set)
		u.btn("sticker:" + list[0].ChatID + "/" + list[0].ID).Click()
		frame()
	}

	react()
	if u.picker.open || stickers() != 1 {
		t.Fatalf("by default: picker open %v, %d sticker reactions", u.picker.open, stickers())
	}

	u.setPerfPref(prefKeepPicker, "on")
	react()
	list := u.stickerList(u.picker.stickerSet)
	u.btn("sticker:" + list[0].ChatID + "/" + list[0].ID).Click()
	frame()
	if !u.picker.open || u.picker.mode != pickReaction || stickers() != 3 {
		t.Fatalf("kept open: picker open %v, mode %v, %d sticker reactions", u.picker.open, u.picker.mode, stickers())
	}
}
