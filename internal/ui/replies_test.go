package ui

import (
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// The pill under an announcement opens its replies, where members write
// theirs.
func TestReplies(t *testing.T) {
	ft := newFloatTest("work-ann")
	m := ft.msgs[len(ft.msgs)-1]
	if m.Replies != 2 || repliesLabel(m.Replies) != "2 replies" || repliesLabel(1) != "1 reply" {
		t.Fatalf("%d replies", m.Replies)
	}
	ft.btn("replies:" + m.ChatID + "/" + m.ID).Click()
	ft.frames(5)
	s := &ft.replies
	if !s.isOpen() || len(s.list) != 2 || len(s.msgs) != 2 || ft.msgInfo.open {
		t.Fatalf("open %v: %+v", s.isOpen(), s.list)
	}
	s.ed.SetText("  Thanks!  ")
	s.send.Click()
	ft.frames(2)
	ft.applyEvents()
	ft.frames(2)
	if n := len(s.list); n != 3 || s.list[2].Text != "Thanks!" || !s.list[2].Me || s.ed.Text() != "" || !s.msgs[2].FromMe {
		t.Fatalf("after sending: %+v, field %q", s.list, s.ed.Text())
	}
	if s.msg.Replies != 3 {
		t.Errorf("the announcement counts %d", s.msg.Replies)
	}
	// Someone else's comes while it's open.
	ft.b.Reply(s.msg, "clara", "Clara", "And the docs?")
	ft.applyEvents()
	ft.frames(2)
	if len(s.list) != 4 {
		t.Errorf("%d replies", len(s.list))
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
	// What was being written waits for the next time.
	s.ed.SetText("And")
	ft.escape()
	if s.isOpen() {
		t.Error("Esc left it open")
	}
	for range 100 {
		ft.frames(1)
	}
	if ft.selected == nil {
		t.Error("Esc closed the chat too")
	}
	ft.openReplies(m)
	ft.frames(2)
	if !s.isOpen() || s.ed.Text() != "And" {
		t.Errorf("open %v, field %q", s.isOpen(), s.ed.Text())
	}
	// A sender's picture leaves for their contact info.
	ft.btn("sender:" + s.msgs[0].ID).Click()
	ft.frames(2)
	if s.isOpen() || !ft.info.open || ft.info.chatID != "clara" {
		t.Errorf("open %v, info %v of %q", s.isOpen(), ft.info.open, ft.info.chatID)
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
