package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/chomosuke9/wazzapclients/internal/auto"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// pickMember picks the first person (not you, @all or @admin) the open
// mention picker offers, and returns their name and ID.
func (st *slashTest) pickMember() (name, jid string) {
	st.t.Helper()
	ms := st.u.mentionQuery()
	if ms == nil {
		st.t.Fatal("no mention picker")
	}
	pick := 0
	for ms.members[pick].Me || strings.HasPrefix(ms.members[pick].ID, "@") {
		pick++
	}
	name, jid = st.u.mentionName(ms.members[pick]), ms.members[pick].ID
	st.u.pickMention(pick)
	st.frame()
	return name, jid
}

// The send button's menu offers Schedule message and View once, for
// text only; Ctrl+Shift+Enter opens the dialog, which schedules the
// message with its mentions and empties the composer.
func TestScheduleDialog(t *testing.T) {
	st := newSlashTest(t, "work")
	st.u.openSendMenu()
	if st.u.ctx.kind != ctxNone {
		t.Fatal("the menu opened over an empty composer")
	}
	st.typeText("standup @")
	name, jid := st.pickMember()
	st.u.conv.composer.Insert("please")
	st.frame()
	st.u.openSendMenu()
	items := st.u.sendMenuItems()
	if st.u.ctx.kind != ctxSend || len(items) != 2 || items[0].label != "Schedule message" || items[1].label != "View once" {
		t.Fatalf("menu %v %+v", st.u.ctx.kind, items)
	}
	st.u.ctx = ctxMenu{}
	st.pressMod(key.NameReturn, key.ModShortcut|key.ModShift)
	if st.u.dialog.kind != dialogSchedule {
		t.Fatal("Ctrl+Shift+Enter didn't open Schedule message")
	}
	// An hour from now, to the next 5 minutes.
	if at, problem := st.u.scheduleTime(); problem != "" || at.Before(testNow().Add(time.Hour)) ||
		at.After(testNow().Add(time.Hour+5*time.Minute)) || at.Minute()%5 != 0 {
		t.Fatalf("suggested %v (%q)", at, problem)
	}
	// Too soon, and too far, aren't taken.
	st.u.setScheduleTime(testNow().Add(5 * time.Minute))
	st.u.confirmSchedule()
	if _, problem := st.u.scheduleTime(); problem == "" || len(st.b.Scheduled("")) != 0 {
		t.Fatal("scheduled 5 minutes ahead")
	}
	st.u.setScheduleTime(testNow().Add(15 * 24 * time.Hour))
	if _, problem := st.u.scheduleTime(); problem == "" {
		t.Fatal("took 15 days ahead")
	}
	// A quick pick, then the send button.
	picks := schedulePicks(st.u.now().Local())
	st.u.setScheduleTime(picks[len(picks)-2].at) // tomorrow morning
	st.u.confirmSchedule()
	st.u.applyEvents()
	if st.text() != "" || st.u.dialog.isOpen() {
		t.Fatalf("composer kept %q, dialog open %v", st.text(), st.u.dialog.isOpen())
	}
	list := st.b.Scheduled("work")
	if len(list) != 1 {
		t.Fatalf("%d scheduled", len(list))
	}
	s := list[0]
	user, _, _ := strings.Cut(jid, "@")
	if s.Draft.Text != "standup @"+user+" please" || len(s.Draft.Mentions) != 1 || s.Draft.Mentions[0] != jid ||
		s.Shown != "standup @"+name+" please" || s.At.Hour() != 8 {
		t.Fatalf("scheduled %+v", s)
	}
}

// View once from the send button's menu sends the text as view once.
func TestSendViewOnceText(t *testing.T) {
	st := newSlashTest(t, "rina")
	st.typeText("only once")
	st.u.openSendMenu()
	st.u.sendMenuItems()[1].run()
	st.u.applyEvents()
	last := st.b.Messages("rina", 1)[0]
	if st.text() != "" || last.Kind != model.KindViewOnce || last.Text != "only once" {
		t.Fatalf("composer %q, sent %+v", st.text(), last)
	}
	// Your own can't be opened; one from someone else opens in a dialog.
	if st.u.canOpenViewOnce(last) {
		t.Fatal("your own view once text opens")
	}
	theirs := &model.Message{ID: "x", ChatID: "rina", Kind: model.KindViewOnce, Text: "a secret"}
	st.u.openViewOnce(theirs)
	if st.u.dialog.kind != dialogConfirm || !st.u.dialog.viewOnce || st.u.dialog.body != "a secret" {
		t.Fatalf("dialog %+v", st.u.dialog)
	}
	// One that came without its text opens on the phone.
	if st.u.canOpenViewOnce(&model.Message{Kind: model.KindViewOnce}) {
		t.Fatal("an empty view once message opens")
	}
}

// Holding the send button opens its menu, and letting go doesn't send.
func TestSendButtonHold(t *testing.T) {
	st := newSlashTest(t, "rina")
	st.typeText("later")
	n := len(st.b.Messages("rina", 1000))
	st.u.sched.hold.btn, st.u.sched.hold.fired = &st.u.conv.send, true // as if held long enough
	st.u.conv.send.Click()
	st.frame()
	if len(st.b.Messages("rina", 1000)) != n || st.text() != "later" {
		t.Fatal("letting go after holding sent the message")
	}
	st.u.conv.send.Click()
	st.frame()
	if len(st.b.Messages("rina", 1000)) != n+1 {
		t.Fatal("a click didn't send")
	}
}

func TestSlashAFK(t *testing.T) {
	st := newSlashTest(t, "rina")
	st.typeText("/afk at the gym")
	st.press(key.NameReturn)
	w := st.u.auto.Away()
	if w == nil || w.Reason != "at the gym" {
		t.Fatalf("away %+v", w)
	}
	ns := st.u.slash.notes["rina"]
	if n := ns[len(ns)-1].note; !strings.HasSuffix(n.Text, auto.AwayPlain(w, st.u.now())) {
		t.Fatalf("note %q", n.Text)
	}
	// Sending a message ends it, with a toast.
	st.typeText("hi")
	st.press(key.NameReturn)
	st.u.applyEvents()
	if st.u.auto.Away() != nil || !strings.HasPrefix(st.u.toastMsg.text, "Welcome back") {
		t.Fatalf("still away; toast %q", st.u.toastMsg.text)
	}
	// I'm back on the note says so.
	n := ns[len(ns)-1].note
	n.Buttons[0].Run()
	if n.Text != "You were back already." {
		t.Fatalf("note %q", n.Text)
	}
}

func TestScheduledBubbles(t *testing.T) {
	st := newSlashTest(t, "work")
	st.typeText("standup @")
	name, _ := st.pickMember()
	st.u.openSchedule()
	st.u.confirmSchedule()
	st.u.applyEvents()
	list := st.b.Scheduled("work")
	if len(list) != 1 {
		t.Fatalf("%d scheduled", len(list))
	}
	id := list[0].ID
	// It shows after the newest message, under its chip.
	rows := st.u.rows(st.u.selected)
	if n := len(rows); rows[n-1].kind != rowScheduled || rows[n-1].job.ID != id || rows[n-2].date != scheduledChip {
		t.Fatalf("last rows %+v", rows[n-2:])
	}
	items := st.u.scheduledMenuItems("work", id)
	if len(items) != 4 || items[0].label != "Send now" || items[1].label != "Edit" || items[3].label != "Cancel" {
		t.Fatalf("menu %+v", items)
	}
	// Edit puts it back in the composer, mention and all; Enter picks
	// the time again, at its own.
	items[1].run()
	st.frame()
	if st.text() != "standup @"+name || len(st.u.conv.mentions) != 1 || !st.u.schedEditing() {
		t.Fatalf("editing %q, mentions %+v", st.text(), st.u.conv.mentions)
	}
	st.u.conv.composer.Insert(" moved")
	st.press(key.NameReturn)
	if st.u.dialog.kind != dialogSchedule {
		t.Fatal("Enter didn't open the dialog")
	}
	if at, _ := st.u.scheduleTime(); !at.Equal(list[0].At) {
		t.Fatalf("the dialog offers %v, not its time %v", at, list[0].At)
	}
	st.u.confirmSchedule()
	st.u.applyEvents()
	list = st.b.Scheduled("work")
	if len(list) != 1 || list[0].ID == id || !strings.HasSuffix(list[0].Shown, " moved") || len(list[0].Draft.Mentions) != 1 {
		t.Fatalf("after the edit %+v", list)
	}
	// Send now sends it, and it's gone from the list.
	st.u.scheduledMenuItems("work", list[0].ID)[0].run()
	st.u.applyEvents()
	if last := st.b.Messages("work", 1)[0]; !last.FromMe || !strings.Contains(last.Text, "moved") || len(st.b.Scheduled("")) != 0 {
		t.Fatalf("sent %q, still scheduled %d", last.Text, len(st.b.Scheduled("")))
	}
	// Cancel takes one away, rows and all.
	st.typeText("another")
	st.u.openSchedule()
	st.u.confirmSchedule()
	st.u.applyEvents()
	for _, s := range st.b.Scheduled("work") {
		st.u.scheduledMenuItems("work", s.ID)[3].run()
	}
	st.u.applyEvents()
	for _, r := range st.u.rows(st.u.selected) {
		if r.kind == rowScheduled || r.date == scheduledChip {
			t.Fatal("a cancelled message still shows")
		}
	}
}

func TestAwayBar(t *testing.T) {
	st := newSlashTest(t, "rina")
	st.typeText("/afk ")
	st.press(key.NameReturn)
	if st.u.auto.Away() == nil {
		t.Fatal("not away")
	}
	cl := st.u.btn("away:back")
	cl.Click() // as if clicked; the bar reads it next frame
	st.frame()
	if st.u.auto.Away() != nil || st.u.toastMsg.text != auto.BackText(0) {
		t.Fatalf("still away; toast %q", st.u.toastMsg.text)
	}
}

// The send view's files are scheduled from its send button's menu, each
// with its caption, and show as files among the scheduled messages.
func TestScheduleFiles(t *testing.T) {
	st := newSlashTest(t, "rina")
	dir := t.TempDir()
	var files []*attachFile
	for _, name := range []string{"plan.pdf", "budget.xlsx"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		files = append(files, &attachFile{Attachment: model.Attachment{Path: p, Media: model.MediaDocument}})
	}
	st.u.addFiles("rina", files)
	st.frame()
	st.u.conv.composer.SetText("for monday")
	st.u.openSendMenu()
	items := st.u.sendMenuItems()
	if st.u.ctx.kind != ctxSend || len(items) != 1 || items[0].label != "Schedule message" {
		t.Fatalf("menu %v %+v", st.u.ctx.kind, items)
	}
	st.u.ctx = ctxMenu{}
	items[0].run()
	if st.u.dialog.kind != dialogSchedule {
		t.Fatal("Schedule message didn't open")
	}
	if p := st.u.schedulePreview(); p != "plan.pdf · for monday\nbudget.xlsx" {
		t.Fatalf("preview %q", p)
	}
	st.u.confirmSchedule()
	st.u.applyEvents()
	for range 30 {
		st.frame()
	}
	if len(st.u.attach.files) != 0 || st.u.dialog.isOpen() {
		t.Fatal("the send view or the dialog stayed open")
	}
	list := st.b.Scheduled("rina")
	if len(list) != 2 || list[0].Media != model.MediaDocument || list[0].FileName != "plan.pdf" ||
		list[0].Text() != "for monday" || list[1].FileName != "budget.xlsx" || list[1].Text() != "" {
		t.Fatalf("scheduled %+v", list)
	}
	// They show; a file can be cancelled, not sent now or edited.
	rows := st.u.rows(st.u.selected)
	if n := len(rows); rows[n-1].kind != rowScheduled || rows[n-2].kind != rowScheduled {
		t.Fatal("the scheduled files don't show")
	}
	for _, it := range st.u.scheduledMenuItems("rina", list[0].ID) {
		if it.key == "sendnow" || it.key == "edit" {
			t.Fatalf("a scheduled file offers %s", it.label)
		}
	}
}
