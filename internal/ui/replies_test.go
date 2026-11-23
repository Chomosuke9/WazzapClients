package ui

import (
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// An announcement's button opens its replies, where members write theirs.
func TestReplies(t *testing.T) {
	ft := newFloatTest("work-ann")
	m := ft.msgs[len(ft.msgs)-1]
	if m.Replies != 2 || repliesLabel(m.Replies) != "2 replies" || repliesLabel(0) != "Reply" || repliesLabel(1) != "1 reply" {
		t.Fatalf("%d replies", m.Replies)
	}
	// Its only button.
	ft.btn("mbtn:" + m.ID + ":0").Click()
	ft.frames(5)
	s := &ft.msgInfo
	if !s.open || !s.replies || len(s.replyList) != 2 {
		t.Fatalf("open %v, replies %v: %+v", s.open, s.replies, s.replyList)
	}
	s.replyEd.SetText("  Thanks!  ")
	s.replySend.Click()
	ft.frames(2)
	ft.applyEvents()
	ft.frames(2)
	if n := len(s.replyList); n != 3 || s.replyList[2].Text != "Thanks!" || !s.replyList[2].Me || s.replyEd.Text() != "" {
		t.Fatalf("after sending: %+v, field %q", s.replyList, s.replyEd.Text())
	}
	if s.msg.Replies != 3 {
		t.Errorf("the announcement counts %d", s.msg.Replies)
	}
	// Someone else's comes while it's open.
	ft.b.Reply(s.msg, "clara", "Clara", "And the docs?")
	ft.applyEvents()
	ft.frames(2)
	if len(s.replyList) != 4 {
		t.Errorf("%d replies", len(s.replyList))
	}
	labels := func(r model.Reply) (out []string) {
		for _, it := range ft.replyItemsOf(r) {
			out = append(out, it.label)
		}
		return out
	}
	if got := labels(model.Reply{Me: true, Receipt: model.Failed}); len(got) != 3 || got[1] != "Send again" || got[2] != "Delete" {
		t.Errorf("a failed reply's menu: %v", got)
	}
	if got := labels(model.Reply{Me: true, Receipt: model.Sent}); len(got) != 2 {
		t.Errorf("your reply's menu: %v", got)
	}
	ft.escape()
	ft.frames(5)
	if s.open {
		t.Error("Esc left it open")
	}
	// Not under other chats' messages.
	ft.SelectID("rina")
	for _, it := range ft.messageMenuItems(ft.selected, ft.msgs[0]) {
		if it.key == "replies" {
			t.Error("a chat's message takes replies")
		}
	}
}

// Pinning asks for how long; unpinning doesn't.
func TestPinDuration(t *testing.T) {
	ft := newFloatTest("work")
	m := ft.msgs[0]
	ft.togglePin(m)
	if ft.ctx.kind != ctxPin {
		t.Fatalf("menu %v", ft.ctx.kind)
	}
	items := ft.pinItems(m)
	if len(items) != 1+len(pinChoices) || !items[0].note {
		t.Fatalf("items %+v", items)
	}
	items[3].run()
	ft.applyEvents()
	if !ft.conv.pins.has(m.ID) || pinChoices[2].d != 30*24*time.Hour {
		t.Error("not pinned")
	}
	ft.closeMenu()
	ft.togglePin(ft.conv.pins.shown())
	ft.applyEvents()
	if ft.conv.pins.has(m.ID) || ft.ctx.isOpen() {
		t.Error("not unpinned")
	}
}

// What highlights selected messages follows the accent.
func TestSelectColor(t *testing.T) {
	ft := newFloatTest("work")
	p := *ft.pal
	p.Green = rgb(0xcba6f7)
	ft.pal = &p
	if c := ft.selColor(); c.R != 0xcb || c.G != 0xa6 || c.B != 0xf7 || c.A == 0xff {
		t.Errorf("select color %v", c)
	}
	if c := ft.flashColor(); c.R != 0xcb || c.A == 0xff {
		t.Errorf("flash color %v", c)
	}
}
