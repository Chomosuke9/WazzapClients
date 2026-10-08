package ui

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestReactions checks the pill's counts, the list of who reacted (you
// first, a tab per emoji), and that clicking your own row takes your
// reaction back.
func TestReactions(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("work")
	u.applyEvents()
	msg := func() *model.Message {
		t.Helper()
		for _, m := range u.msgs {
			if strings.HasPrefix(m.Text, "Release candidate") {
				return m
			}
		}
		t.Fatal("no release candidate message")
		return nil
	}
	counts, total := reactionCounts([]*model.Message{msg()})
	if total != 4 || len(counts) != 3 || counts[0] != (model.ReactionCount{Emoji: "👍", Count: 2}) || msg().MyReaction != "👍" {
		t.Fatalf("reactions = %v (%d), mine %q", counts, total, msg().MyReaction)
	}

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
	}
	frame()
	u.mouse = image.Point{} // opens below the click, at 8,14
	u.openReactions([]*model.Message{msg()})
	frame()
	list := u.reacts.list
	if len(list) != 4 || !list[0].Me || list[0].Emoji != "👍" || list[0].Name != "You" {
		t.Fatalf("reactors = %+v, want 4 with you first", list)
	}

	// Your row is the first under the tabs: 52 tall, then 8 of padding.
	// The content starts under the title bar: the pointer's offset says
	// where.
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(100, 200)})
	frame()
	p := f32.Pt(100, float32(200-u.mouse.Y+14+52+8+32))
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
	frame()
	r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p, Time: time.Second})
	r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p, Time: time.Second + 50*time.Millisecond})
	frame()
	u.applyEvents()
	frame()
	if m := msg(); m.MyReaction != "" || m.ReactionTotal() != 3 {
		t.Fatalf("after removing yours: mine %q, %v", m.MyReaction, m.Reactions)
	}
	if !u.reacts.isOpen() || len(u.reacts.list) != 3 || u.reacts.list[0].Me {
		t.Fatalf("the list after removing yours: open %v, %+v", u.reacts.isOpen(), u.reacts.list)
	}
	u.Escape()
	frame()
	if u.reacts.msgs != nil {
		t.Error("Esc left the list open")
	}
}
