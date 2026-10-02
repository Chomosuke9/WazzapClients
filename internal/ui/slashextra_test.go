package ui

import (
	"strings"
	"testing"

	"gioui.org/io/key"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// runConfirm presses the confirm dialog's first button.
func (st *slashTest) runConfirm(title string) {
	st.t.Helper()
	d := &st.u.dialog
	if d.kind != dialogConfirm || d.title != title {
		st.t.Fatalf("dialog %q, want %q", d.title, title)
	}
	d.buttons[0].run()
	st.u.dialog = dialogState{}
	st.u.applyEvents()
	st.frame()
}

func TestSlashPurge(t *testing.T) {
	st := newSlashTest(t, "work")
	all := st.b.Messages("work", 1000)
	st.typeText("/purge 3")
	st.press(key.NameReturn)
	st.runConfirm("Delete 3 messages for everyone?")
	now := st.b.Messages("work", 1000)
	for i, m := range now {
		if deleted := m.Kind == model.KindDeleted; deleted != (i >= len(all)-3) {
			t.Errorf("message %d (%q) deleted: %v", i, all[i].Text, deleted)
		}
	}
	// Deleted messages don't count: replying to the 5th from the end
	// deletes it and the one before.
	st.u.conv.reply = now[len(now)-5]
	st.typeText("/purge 2")
	st.press(key.NameReturn)
	st.runConfirm("Delete 2 messages for everyone?")
	now = st.b.Messages("work", 1000)
	for i, m := range now {
		want := i >= len(now)-3 || i == len(now)-5 || i == len(now)-6
		if deleted := m.Kind == model.KindDeleted; deleted != want {
			t.Errorf("after the reply, message %d deleted: %v", i, deleted)
		}
	}
	if st.u.conv.reply != nil {
		t.Error("the reply stayed in the composer")
	}
}

func TestSlashPurgeOthers(t *testing.T) {
	// In a one-to-one chat only your own messages go.
	st := newSlashTest(t, "rina")
	all := st.b.Messages("rina", 1000)
	mine := 0
	for _, m := range all[len(all)-4:] {
		if m.FromMe && st.u.now().Sub(m.Time) < 48*60*60*1e9 {
			mine++
		}
	}
	st.typeText("/purge 4")
	st.press(key.NameReturn)
	if mine == 0 {
		if st.u.dialog.kind == dialogConfirm {
			t.Fatal("asked to delete none of yours")
		}
		return
	}
	if !strings.Contains(st.u.dialog.body, "from other people") || strings.Contains(st.u.dialog.body, "admins") {
		t.Errorf("body %q", st.u.dialog.body)
	}
	st.runConfirm(st.u.dialog.title)
	for _, m := range st.b.Messages("rina", 1000) {
		if m.Kind == model.KindDeleted && !m.FromMe {
			t.Errorf("deleted Rina's %q", m.ID)
		}
	}
}

func TestSlashRaffleAndCalc(t *testing.T) {
	st := newSlashTest(t, "work")
	last := func() *model.Message {
		ms := st.b.Messages("work", 1)
		return ms[len(ms)-1]
	}
	st.typeText("/raffle 2")
	st.press(key.NameReturn)
	if m := last(); !m.FromMe || !strings.HasPrefix(m.Text, "🎲 *Raffle*\n") || !strings.Contains(m.Text, ":\n1. ") ||
		strings.Count(m.Text, "⁨@") != 2 {
		t.Fatalf("raffle sent %q", m.Text)
	}
	st.typeText("/calc 12 x 4500")
	st.press(key.NameReturn)
	if m := last(); m.Text != "12 x 4500 = 54 000" {
		t.Fatalf("calc sent %q", m.Text)
	}
	st.typeText("/calc 1/0")
	st.press(key.NameReturn)
	ns := st.u.slash.notes["work"]
	if len(ns) == 0 || !ns[len(ns)-1].note.Failed || last().Text != "12 x 4500 = 54 000" {
		t.Fatal("/calc 1/0 didn't fail")
	}
}

func TestSlashCalcLive(t *testing.T) {
	st := newSlashTest(t, "work")
	st.typeText("/calc 2 x 3")
	if h := st.u.slashHint(st.u.slashQuery()); h != " = 6" {
		t.Fatalf("hint %q", h)
	}
	st.typeText("/calc 2 x")
	if sp := st.u.slashQuery(); sp.preview != "" || st.u.slashHint(sp) != "" {
		t.Fatalf("an unfinished sum previews %q", sp.preview)
	}
	st.typeText("/calc 2 x 3")
	st.press(key.NameReturn)
	// ans is the last answer.
	st.typeText("/calc ans x 2")
	st.press(key.NameReturn)
	ms := st.b.Messages("work", 1)
	if got := ms[len(ms)-1].Text; got != "ans x 2 = 12" {
		t.Fatalf("sent %q", got)
	}
}

func TestCalcPad(t *testing.T) {
	st := newSlashTest(t, "work")
	st.typeText("/calc ")
	press := func(keys ...string) {
		for _, k := range keys {
			sp := st.u.slashQuery()
			if !isCalcPick(sp) {
				t.Fatalf("no calculator for %q", st.text())
			}
			st.u.pressCalc(sp, k)
			st.frame()
		}
	}
	for _, c := range []struct {
		keys []string
		want string
	}{
		{[]string{"1", "2", "×", "3"}, "/calc 12 × 3"},
		{[]string{"+", "−"}, "/calc 12 × 3 − "}, // an operator replaces the last
		{[]string{"⌫"}, "/calc 12 × 3"},         // the operator goes whole
		{[]string{"()", "4", "+", "000", "()"}, "/calc 12 × 3 × (4 + 000)"},
		{[]string{"%"}, "/calc 12 × 3 × (4 + 000)%"},
		{[]string{"C", "−", "5"}, "/calc -5"},
		{[]string{"C", "⌫", "⌫"}, "/calc "}, // nothing left to delete
		{[]string{"2", "xʸ", "1", "0", "mod", "7"}, "/calc 2 ^ 10 mod 7"},
		{[]string{"⌫", "⌫"}, "/calc 2 ^ 10"}, // mod goes whole
		{[]string{"×", "mod"}, "/calc 2 ^ 10 mod "},
		{[]string{"C", "round", "5", "÷", "3", ",", "2", "()"}, "/calc round(5 ÷ 3, 2)"},
		{[]string{"ans", "√", "9"}, "/calc round(5 ÷ 3, 2) × ans × √9"},
		{[]string{"C", "max", "⌫"}, "/calc "},      // a function goes whole
		{[]string{"5", "!", "!", ","}, "/calc 5!"}, // one !, and no comma outside a function
		{[]string{"π"}, "/calc 5! × π"},
	} {
		press(c.keys...)
		if st.text() != c.want {
			t.Fatalf("after %v: %q, want %q", c.keys, st.text(), c.want)
		}
	}
	// The amounts of the message replied to type with + between them.
	st.u.conv.reply = &model.Message{ID: "bill", ChatID: "work", Sender: "Dewi", Text: "Rice 25,000, tea 8K"}
	if ns := st.u.replyNumbers(); len(ns) != 2 || ns[0] != 25000 {
		t.Fatalf("numbers %v", ns)
	}
	press("C")
	sp := st.u.slashQuery()
	st.u.calcInsertNumber(sp, "25000")
	st.frame()
	st.u.calcInsertNumber(st.u.slashQuery(), "8000")
	st.frame()
	press("÷", "2")
	if st.text() != "/calc 25000 + 8000 ÷ 2" {
		t.Fatalf("with chips: %q", st.text())
	}
	if sp := st.u.slashQuery(); !sp.previewOK || calcAnswer(sp) != "29000" {
		t.Fatalf("answer %q", sp.preview)
	}
	st.press(key.NameReturn)
	ms := st.b.Messages("work", 1)
	if got := ms[len(ms)-1].Text; got != "25000 + 8000 ÷ 2 = 29 000" {
		t.Fatalf("sent %q", got)
	}
}
