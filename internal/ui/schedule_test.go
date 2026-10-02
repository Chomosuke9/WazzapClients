package ui

import (
	"strings"
	"testing"

	"gioui.org/io/key"

	"github.com/chomosuke9/wazzapclients/internal/auto"
)

func TestSlashSchedule(t *testing.T) {
	st := newSlashTest(t, "work")
	// The time's suggestions, then the message, where @ offers members.
	st.typeText("/schedule ")
	if sp := st.u.slashQuery(); sp == nil || len(sp.vals) != 4 || sp.vals[0].name != "30m" {
		t.Fatalf("suggestions %+v", sp)
	}
	st.press(key.NameDownArrow)
	st.press(key.NameReturn)
	if st.text() != "/schedule 1h " {
		t.Fatalf("picked %q", st.text())
	}
	if h := st.u.slashHint(st.u.slashQuery()); !strings.Contains(h, "message") {
		t.Fatalf("hint %q", h)
	}
	ed := &st.u.conv.composer
	ed.Insert("standup @")
	st.frame()
	ms := st.u.mentionQuery()
	if ms == nil {
		t.Fatal("no mention picker in the message")
	}
	pick := 0
	for ms.members[pick].Me || strings.HasPrefix(ms.members[pick].ID, "@") { // @all, @admin
		pick++
	}
	name, jid := st.u.mentionName(ms.members[pick]), ms.members[pick].ID
	st.u.pickMention(pick)
	st.frame()
	ed.Insert("please")
	st.frame()
	if st.u.mentionQuery() != nil {
		t.Fatal("the mention picker stayed")
	}
	if sp := st.u.slashQuery(); !sp.previewOK || !strings.HasPrefix(sp.preview, "Sends today at 16:00") {
		t.Fatalf("preview %q", sp.preview)
	}
	// The command's own text has no mentions to offer.
	st.typeText("/schedule @")
	if st.u.mentionQuery() != nil {
		t.Fatal("mentions offered for the time")
	}
	st.typeText("/schedule 1h standup @" + name + " please")
	st.press(key.NameReturn)
	if st.text() != "" {
		t.Fatalf("composer kept %q", st.text())
	}
	jobs := st.u.auto.Jobs("work")
	if len(jobs) != 1 {
		t.Fatalf("%d jobs", len(jobs))
	}
	j := jobs[0]
	user, _, _ := strings.Cut(jid, "@")
	if j.Text != "standup @"+user+" please" || len(j.Mentions) != 1 || j.Mentions[0] != jid ||
		j.Shown != "standup @"+name+" please" || !j.At.Equal(st.u.auto.Jobs("")[0].At) {
		t.Fatalf("job %+v", j)
	}
	ns := st.u.slash.notes["work"]
	n := ns[len(ns)-1].note
	if !strings.HasPrefix(n.Text, "Sends today at 16:00, in 1h:\nstandup @") || len(n.Buttons) != 2 {
		t.Fatalf("note %+v", n)
	}
	// /scheduled lists it; Send now sends it.
	st.typeText("/scheduled ")
	st.press(key.NameReturn)
	ns = st.u.slash.notes["work"]
	listed := ns[len(ns)-1].note
	if len(listed.Buttons) != 2 || listed.Buttons[0].Label != "Send now" {
		t.Fatalf("listed %+v", listed)
	}
	listed.Buttons[0].Run()
	last := st.b.Messages("work", 1)[0]
	if !last.FromMe || !strings.Contains(last.Text, "standup") || listed.Text != "Sent:\nstandup @"+name+" please" {
		t.Fatalf("sent %q, note %q", last.Text, listed.Text)
	}
	// The first note's buttons know it's gone.
	n.Buttons[1].Run()
	if !strings.HasPrefix(n.Text, "It was sent already") {
		t.Fatalf("note %q", n.Text)
	}
	st.typeText("/scheduled ")
	st.press(key.NameReturn)
	ns = st.u.slash.notes["work"]
	if got := ns[len(ns)-1].note.Text; got != "Nothing is scheduled in this chat." {
		t.Fatalf("listed %q", got)
	}
}

func TestSlashScheduleProblems(t *testing.T) {
	st := newSlashTest(t, "rina")
	st.typeText("/schedule tomorrow")
	if sp := st.u.slashQuery(); sp.preview != "" || sp.in.Problem() == "" {
		t.Fatalf("preview %q, problem %q", sp.preview, sp.in.Problem())
	}
	st.typeText("/schedule 09:00") // no message
	st.press(key.NameReturn)
	if st.text() == "" || len(st.u.auto.Jobs("")) != 0 {
		t.Fatal("scheduled without a message")
	}
	st.typeText("/schedule 1/1/2020 10:00 old")
	st.press(key.NameReturn)
	ns := st.u.slash.notes["rina"]
	if len(ns) == 0 || !ns[len(ns)-1].note.Failed || ns[len(ns)-1].note.Text != "That time has passed." {
		t.Fatal("scheduled in the past")
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
	st.typeText("/schedule 1h standup @")
	ms := st.u.mentionQuery()
	pick := 0
	for ms.members[pick].Me || strings.HasPrefix(ms.members[pick].ID, "@") {
		pick++
	}
	name := st.u.mentionName(ms.members[pick])
	st.u.pickMention(pick)
	st.frame()
	st.press(key.NameReturn)
	jobs := st.u.auto.Jobs("work")
	if len(jobs) != 1 {
		t.Fatalf("%d jobs", len(jobs))
	}
	// It shows after the newest message, under its chip.
	rows := st.u.rows(st.u.selected)
	if n := len(rows); rows[n-1].kind != rowScheduled || rows[n-1].job.ID != jobs[0].ID || rows[n-2].date != scheduledChip {
		t.Fatalf("last rows %+v", rows[n-2:])
	}
	items := st.u.scheduledMenuItems(jobs[0].ID)
	if len(items) != 4 || items[0].label != "Send now" || items[1].label != "Edit" || items[3].label != "Cancel" {
		t.Fatalf("menu %+v", items)
	}
	// Edit puts it back in the composer, mention and all.
	items[1].run()
	st.frame()
	want := "/schedule today 16:00 standup @" + name
	if st.text() != want || len(st.u.conv.mentions) != 1 || st.u.slash.replacing != jobs[0].ID {
		t.Fatalf("editing %q, mentions %+v", st.text(), st.u.conv.mentions)
	}
	// Running it replaces the old one.
	st.typeText("/schedule tomorrow 09:00 standup @" + name + " moved")
	st.press(key.NameReturn)
	jobs = st.u.auto.Jobs("work")
	if len(jobs) != 1 || !strings.HasSuffix(jobs[0].Shown, " moved") || len(jobs[0].Mentions) != 1 {
		t.Fatalf("after the edit %+v", jobs)
	}
	// An edit given up leaves it be.
	st.u.scheduledMenuItems(jobs[0].ID)[1].run()
	st.frame()
	st.typeText("never mind")
	st.typeText("/schedule 2h another")
	st.press(key.NameReturn)
	if len(st.u.auto.Jobs("work")) != 2 {
		t.Fatal("a given-up edit replaced the message")
	}
	// Cancel takes it away, rows and all.
	for _, j := range st.u.auto.Jobs("work") {
		st.u.scheduledMenuItems(j.ID)[3].run()
	}
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
