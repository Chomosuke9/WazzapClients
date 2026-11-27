package ui

import (
	"slices"
	"strings"
	"testing"

	"gioui.org/io/key"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// customTest opens the demo's "work" group with custom mentions on.
func customTest(t *testing.T) *slashTest {
	st := newSlashTest(t, "work")
	st.u.extendedMentions = true
	return st
}

// membersOf returns the IDs of the others in a group, and yours.
func membersOf(t *testing.T, u *UI, chat string) (others []string, me string) {
	info := u.chatMembers(chat)
	if info == nil {
		t.Fatal("no members")
	}
	for _, m := range info.Members {
		if m.Me {
			me = m.ID
		} else {
			others = append(others, m.ID)
		}
	}
	if len(others) < 3 {
		t.Fatalf("%d others", len(others))
	}
	return others, me
}

func customRow(ms *mentionState) int {
	return slices.IndexFunc(ms.members, func(m model.Member) bool { return strings.HasPrefix(m.ID, mentionCustomPrefix) })
}

// One switch on the Extra features page, Extended mentions, turns on @admin and
// custom mentions together. It's off until then, and remembered.
func TestCustomMentionsExtra(t *testing.T) {
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	u.applyEvents()
	if u.extendedMentions {
		t.Fatal("on by default")
	}
	u.SelectID("work")
	others, _ := membersOf(t, u, "work")
	u.setCustomMentions("work", []customMention{{ID: "1", Name: "Tim frontend", Members: others[:2]}})
	hits := func() *mentionState {
		ed := &u.conv.composer
		ed.SetText("@")
		ed.SetCaret(1, 1)
		ms := u.mentionQuery()
		if ms == nil {
			t.Fatal("no mention picker for @")
		}
		return ms
	}
	offered := func() bool { return customRow(hits()) >= 0 }
	adminOffered := func() bool {
		return slices.ContainsFunc(hits().members, func(m model.Member) bool { return m.ID == mentionAdminID })
	}
	if offered() || adminOffered() {
		t.Error("offered while off")
	}
	if slices.ContainsFunc(u.convMenuItems(u.selected), func(it menuItem) bool { return it.key == "mentions" }) {
		t.Error("in the ⋮ menu while off")
	}
	u.ShowPage("extras")
	settingRowByKey(t, u, prefExtendedMentions).run()
	if b.Pref(prefExtendedMentions) != "on" || !offered() || !adminOffered() {
		t.Error("not offered once on")
	}
	if !slices.ContainsFunc(u.convMenuItems(u.selected), func(it menuItem) bool { return it.key == "mentions" }) {
		t.Error("not in the ⋮ menu once on")
	}
	u2 := New(b)
	if !u2.extendedMentions {
		t.Error("the switch wasn't remembered")
	}
	if l := u2.customMentionsOf("work"); len(l) != 1 || l[0].Name != "Tim frontend" || !slices.Equal(l[0].Members, others[:2]) {
		t.Errorf("the mention wasn't remembered: %+v", l)
	}
}

// Picking one sends like @admin: "@<group JID>" under the name, its people
// mentioned, and it takes the whole name out of what the picker looks for.
func TestCustomMentionSend(t *testing.T) {
	st := customTest(t)
	others, me := membersOf(t, st.u, "work")
	picked := []string{others[0], others[2], me, "gone@lid"}
	st.u.setCustomMentions("work", []customMention{{ID: "1", Name: "Tim frontend", Members: picked}})

	st.typeText("Hi @tim fr")
	ms := st.u.mentionQuery()
	if ms == nil {
		t.Fatal("no picker")
	}
	i := customRow(ms)
	if i < 0 {
		t.Fatalf("not offered: %+v", ms.members)
	}
	if ms.members[i].Name != "Tim frontend" {
		t.Fatalf("row %+v", ms.members[i])
	}
	st.u.conv.mentionSel = i
	st.press(key.NameReturn)
	if st.text() != "Hi @Tim frontend " {
		t.Fatalf("composer %q", st.text())
	}
	if st.u.mentionQuery() != nil {
		t.Fatal("the picker stayed after picking")
	}

	d := st.u.draftFrom(trimSpace(st.text()))
	if d.Text != "Hi @work" || d.MentionChat != "Tim frontend" || d.MentionAdmins {
		t.Fatalf("draft %+v", d)
	}
	if !slices.Equal(d.Mentions, []string{others[0], others[2]}) {
		t.Fatalf("mentions %v, want %v without you or who left", d.Mentions, []string{others[0], others[2]})
	}

	// In the chat it shows the name, without a pill on your own message.
	st.press(key.NameReturn)
	last := st.u.msgs[len(st.u.msgs)-1]
	if !strings.Contains(last.Text, string(model.MentionGroup)+"@Tim frontend") {
		t.Fatalf("sent %q", last.Text)
	}
	if got := mentionMarks.Replace(last.Text); strings.ContainsRune(got, model.MentionGroup) {
		t.Fatalf("mark left in %q", got)
	}
}

// A message shows one name for each group JID: the first name gets the
// group's, the others JIDs made up from it, and everyone of them is notified.
func TestCustomMentionTwoInOneMessage(t *testing.T) {
	st := customTest(t)
	others, _ := membersOf(t, st.u, "work")
	refs := []mentionRef{
		{name: "Tim frontend", jid: mentionCustomID, people: others[:1]},
		{name: "Tim", jid: mentionCustomID, people: others[1:2]},
		{name: "admin", jid: mentionAdminID},
	}
	d := st.u.draftWith("@Tim frontend @admin", refs, "work")
	second := "@" + model.GroupNameJID("work", 1)
	if d.Text != "@work "+second || d.MentionChat != "Tim frontend" || d.MentionAdmins ||
		!slices.Equal(d.GroupNames, []string{"admin"}) {
		t.Fatalf("draft %+v", d)
	}
	if !slices.Contains(d.Mentions, others[0]) {
		t.Fatalf("mentions %v", d.Mentions)
	}
	info := st.u.chatMembers("work")
	for _, m := range info.Members {
		if m.Admin && !m.Me && !slices.Contains(d.Mentions, m.ID) {
			t.Errorf("admin %s not notified: %v", m.ID, d.Mentions)
		}
	}
	// @all goes its own way, beside a name.
	d = st.u.draftWith("@Tim frontend @all", append(refs[:1:1], mentionRef{name: "all", jid: mentionAllID}), "work")
	if d.Text != "@work @all" || !d.MentionAll || d.MentionChat != "Tim frontend" || len(d.GroupNames) != 0 {
		t.Fatalf("with @all: %+v", d)
	}
	// Two custom mentions both show their names, and notify their people.
	d = st.u.draftWith("@Tim frontend and @Tim", refs[:2], "work")
	if d.Text != "@work and "+second || d.MentionChat != "Tim frontend" || !slices.Equal(d.GroupNames, []string{"Tim"}) {
		t.Fatalf("two: %+v", d)
	}
	if !slices.Contains(d.Mentions, others[0]) || !slices.Contains(d.Mentions, others[1]) {
		t.Fatalf("two: mentions %v", d.Mentions)
	}
	m := st.u.backend.Send("work", d)
	if m == nil || !strings.Contains(m.Text, string(model.MentionGroup)+"@Tim frontend") ||
		!strings.Contains(m.Text, string(model.MentionGroup)+"@Tim⁩") {
		t.Fatalf("sent %+v", m)
	}
}

// Who gets a pill: you when you're one of its people, through the backend's
// mark, and no one else, admin or not. A group mention opens its people,
// a pill of yours or "@all" nothing.
func TestCustomMentionPill(t *testing.T) {
	st := customTest(t)
	for _, c := range []struct {
		marks       string
		pill, opens bool
	}{
		{string(model.MentionNotifies), true, false},
		{string(model.MentionNotifies) + string(model.MentionGroup), true, true},
		{string(model.MentionGroup), false, true},
		{string(model.MentionAdmins), false, true},
	} {
		_, deco := st.u.richSpans("⁨"+c.marks+"@Tim⁩", 15, st.u.pal.Text, false, pillMe)
		if len(deco) != 1 || (deco[0]&decoPill != 0) != c.pill || (deco[0]&decoMention != 0) != c.opens {
			t.Errorf("marks %q: deco %v", c.marks, deco)
		}
	}
}

// Clicking a group mention in a message lists its people in the group's
// info panel, each mention its own when the message says so.
func TestMentionPeoplePanel(t *testing.T) {
	st := customTest(t)
	others, _ := membersOf(t, st.u, "work")
	refs := []mentionRef{
		{name: "Tim frontend", jid: mentionCustomID, people: others[:2]},
		{name: "Tim backend", jid: mentionCustomID, people: others[1:3]},
	}
	d := st.u.draftWith("@Tim frontend @Tim backend", refs, "work")
	if len(d.GroupPeople) != 2 || !slices.Equal(d.GroupPeople[0], others[:2]) || !slices.Equal(d.GroupPeople[1], others[1:3]) {
		t.Fatalf("draft %+v", d)
	}
	m := st.u.backend.Send("work", d)
	st.u.openMention(m, "@Tim backend")
	if !st.u.info.open || st.u.info.chatID != "work" || st.u.info.sub != infoMentioned {
		t.Fatalf("panel %v %q %q", st.u.info.open, st.u.info.chatID, st.u.info.sub)
	}
	if mp := st.u.info.mention; mp.name != "Tim backend" || mp.unsure || !slices.Equal(mp.people, others[1:3]) {
		t.Fatalf("people %+v", mp)
	}
	st.frame()
	for _, id := range others[1:3] {
		if !st.drawn(st.u.btn("mentioned:" + id)) {
			t.Errorf("%s not listed", id)
		}
	}
	// A person opens their contact info.
	st.u.openContact(others[1], "x")
	if st.u.info.sub != "" || st.u.info.chatID != others[1] {
		t.Fatalf("contact: %q %q", st.u.info.sub, st.u.info.chatID)
	}
}

// The editor: its name's rules, picking people, saving, changing, deleting.
func TestCustomMentionEditor(t *testing.T) {
	st := customTest(t)
	others, me := membersOf(t, st.u, "work")
	st.u.openCustomMention("work", nil)
	st.frame()
	d := &st.u.dialog
	rows := st.u.forwardRows(d)
	if len(rows) != len(others) || slices.ContainsFunc(rows, func(r fwdRow) bool { return r.id == me }) {
		t.Fatalf("rows %+v", rows)
	}
	d.listName.SetText("  Tim frontend ")
	st.u.saveCustomMention(d)
	if d.closing || len(st.u.customMentionsOf("work")) != 0 {
		t.Fatal("saved with no one picked")
	}
	st.u.btn("fwd:" + others[0]).Click()
	st.frame()
	st.frame()
	st.u.btn("fwd:" + others[1]).Click()
	st.frame()
	st.frame()
	if !slices.Equal(d.picked, others[:2]) {
		t.Fatalf("picked %v", d.picked)
	}
	st.u.saveCustomMention(d)
	l := st.u.customMentionsOf("work")
	if len(l) != 1 || l[0].Name != "Tim frontend" || !slices.Equal(l[0].Members, others[:2]) {
		t.Fatalf("saved %+v", l)
	}
	if st.b.Pref(prefCustomMentionsOf+"work") == "" {
		t.Fatal("not kept")
	}

	for name, bad := range map[string]bool{
		"Tim backend": false, "tim FRONTEND": true, "All": true, "admin": true, "a@b": true, "a,b": true,
		strings.Repeat("é", maxCustomMentionName):   false,
		strings.Repeat("é", maxCustomMentionName+1): true,
	} {
		if got := st.u.customMentionProblem("work", name, ""); (got != "") != bad {
			t.Errorf("%q: %q", name, got)
		}
	}
	if got := st.u.customMentionProblem("work", "TIM frontend", l[0].ID); got != "" {
		t.Errorf("renaming itself: %q", got)
	}

	// Changing it keeps its ID; the dialog holds it as saved.
	st.u.openCustomMention("work", &l[0])
	d = &st.u.dialog
	if d.listName.Text() != "Tim frontend" || !slices.Equal(d.picked, others[:2]) {
		t.Fatalf("editor holds %q %v", d.listName.Text(), d.picked)
	}
	d.listName.SetText("Tim web")
	d.picked = others[:1]
	st.u.saveCustomMention(d)
	l = st.u.customMentionsOf("work")
	if len(l) != 1 || l[0].ID != "1" || l[0].Name != "Tim web" || !slices.Equal(l[0].Members, others[:1]) {
		t.Fatalf("changed %+v", l)
	}

	// The picker sees the change.
	st.typeText("@tim w")
	if ms := st.u.mentionQuery(); ms == nil || customRow(ms) < 0 {
		t.Fatal("the new name isn't offered")
	}

	st.u.confirmDeleteCustomMention("work", "1")
	if st.u.dialog.kind != dialogConfirm {
		t.Fatal("deleted without asking")
	}
	st.u.dialog.buttons[0].run()
	if len(st.u.customMentionsOf("work")) != 0 || st.b.Pref(prefCustomMentionsOf+"work") != "" {
		t.Fatal("not deleted")
	}
}

func TestCustomMentionWho(t *testing.T) {
	info := &model.ChatInfo{Members: []model.Member{{ID: "me", Name: "Me", Me: true},
		{ID: "a", Name: "Ana"}, {ID: "b", Push: "Budi"}, {ID: "c", Contact: "Cici"}, {ID: "d", Name: "Dodi"}, {ID: "e", Name: "Eka"}}}
	for _, c := range []struct {
		ids  []string
		want string
	}{
		{nil, "No one in this group"},
		{[]string{"me", "gone"}, "No one in this group"},
		{[]string{"a"}, "Ana"},
		{[]string{"a", "b"}, "Ana and Budi"},
		{[]string{"a", "b", "c"}, "Ana, Budi and Cici"},
		{[]string{"a", "b", "c", "d"}, "Ana, Budi, Cici and 1 other"},
		{[]string{"a", "b", "c", "d", "e"}, "Ana, Budi, Cici and 2 others"},
	} {
		if got := customMentionWho(info, c.ids); got != c.want {
			t.Errorf("%v: %q, want %q", c.ids, got, c.want)
		}
	}
}

// A scheduled message comes back to edit with its custom mention, and keeps
// whom it notifies.
func TestCustomMentionSchedule(t *testing.T) {
	st := customTest(t)
	others, _ := membersOf(t, st.u, "work")
	st.u.setCustomMentions("work", []customMention{{ID: "1", Name: "Tim frontend", Members: others[:2]}})
	st.typeText("standup @tim")
	ms := st.u.mentionQuery()
	if ms == nil || customRow(ms) < 0 {
		t.Fatalf("not offered: %+v", ms)
	}
	st.u.pickMention(customRow(ms))
	st.frame()
	st.u.openSchedule()
	st.frame()
	st.u.confirmSchedule()
	st.u.applyEvents()
	jobs := st.u.backend.Scheduled("work")
	if len(jobs) != 1 {
		t.Fatalf("%d scheduled", len(jobs))
	}
	j := jobs[0]
	if d := j.Draft; d.MentionChat != "Tim frontend" || d.Text != "standup @work" || !slices.Equal(d.Mentions, others[:2]) {
		t.Fatalf("scheduled %+v", j)
	}
	st.u.editScheduled(j)
	refs := st.u.conv.mentions
	if len(refs) != 1 || refs[0].jid != mentionCustomID || refs[0].name != "Tim frontend" || !slices.Equal(refs[0].people, others[:2]) {
		t.Fatalf("refs %+v", refs)
	}
}
