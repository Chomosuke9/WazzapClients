package ui

import (
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestGalleryPeriods checks the Media panel's headings: what's under each
// and its dates.
func TestGalleryPeriods(t *testing.T) {
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 12, 0, 0, 0, time.UTC) }
	now := day(2026, time.November, 25)
	for _, c := range []struct {
		t          time.Time
		now        time.Time
		title, sub string
	}{
		{now.Add(time.Hour), now, "Today", "25 November 2026"},
		{day(2026, time.November, 24), now, "Yesterday", "24 November 2026"},
		{day(2026, time.November, 23), now, "Last week", "18 – 23 November 2026"},
		{day(2026, time.November, 18), now, "Last week", "18 – 23 November 2026"},
		{day(2026, time.November, 17), now, "This month", "1 – 17 November 2026"},
		{day(2026, time.October, 31), now, "October", "1 – 31 October 2026"},
		{day(2025, time.December, 2), now, "December", "1 – 31 December 2025"},
		// Early in a month the week reaches into the one before.
		{day(2026, time.November, 1), day(2026, time.November, 4), "Last week", "28 October – 2 November 2026"},
		{day(2026, time.October, 27), day(2026, time.November, 4), "October", "1 – 27 October 2026"},
		{day(2026, time.January, 1), day(2026, time.January, 4), "Last week", "28 December 2025 – 2 January 2026"},
		{day(2026, time.November, 1), day(2026, time.November, 9), "This month", "1 November 2026"},
	} {
		p := periodOf(c.t, c.now)
		sub := dateRange(p.from, p.to.AddDate(0, 0, -1))
		if p.title != c.title || sub != c.sub || !p.has(c.t) {
			t.Errorf("%v on %v: %q, %q (has it: %v), want %q, %q", c.t, c.now, p.title, sub, p.has(c.t), c.title, c.sub)
		}
	}
}

// TestGalleryLines checks that every loaded message is in one line, under
// its heading, and that today's has one only when it isn't at the top.
func TestGalleryLines(t *testing.T) {
	now := testNow()
	var g galleryState
	for _, days := range []int{0, 0, 0, 1, 3, 3, 40} {
		g.list.msgs = append(g.list.msgs, &model.Message{Time: now.AddDate(0, 0, -days)})
	}
	check := func(want []galleryLine) {
		t.Helper()
		got := g.galleryLines(2, now)
		if len(got) != len(want) {
			t.Fatalf("%d lines, want %d: %+v", len(got), len(want), got)
		}
		for i, l := range got {
			l.sub = ""
			if l != want[i] {
				t.Errorf("line %d: %+v, want %+v", i, l, want[i])
			}
		}
	}
	check([]galleryLine{{from: 0, to: 2}, {from: 2, to: 3}, {title: "Yesterday"}, {from: 3, to: 4},
		{title: "Last week"}, {from: 4, to: 6}, {title: "August"}, {from: 6, to: 7}})
	// The oldest first.
	m := g.list.msgs
	g.list.msgs = []*model.Message{m[3], m[0]}
	check([]galleryLine{{title: "Yesterday"}, {from: 0, to: 1}, {title: "Today"}, {from: 1, to: 2}})
	// Starred messages aren't grouped.
	g.tab, g.list.msgs = model.GalleryStarred, m
	if got := g.galleryLines(1, now); len(got) != len(m) || got[0].title != "" {
		t.Errorf("starred: %+v", got)
	}
}

// TestCommunityControls checks that everything on the Communities page
// does something: a name opens the announcements, and View all shows only
// where groups are folded.
func TestCommunityControls(t *testing.T) {
	st := newSlashTest(t, "rina")
	u := st.u
	u.setPage(pageCommunities)
	st.frame()
	c := u.communities[0]
	u.btn("community:" + c.ID).Click()
	st.frame()
	st.frame()
	if u.selected == nil || u.selected.ID != c.Announcements || u.selPage != pageCommunities {
		t.Errorf("the name opened %+v, want %s", u.selected, c.Announcements)
	}
	shown := func(kind int) (n int) {
		for _, e := range u.communityEntries() {
			if e.community == c && e.kind == kind {
				n++
			}
		}
		return n
	}
	c.Groups = []string{"work", "family"}
	if shown(entryViewAll) != 0 || shown(entryGroup) != 3 {
		t.Errorf("two groups: %d View all, %d rows", shown(entryViewAll), shown(entryGroup))
	}
	c.Groups = []string{"work", "family", "design"}
	if shown(entryViewAll) != 1 || shown(entryGroup) != 3 {
		t.Errorf("three groups: %d View all, %d rows", shown(entryViewAll), shown(entryGroup))
	}
	for _, want := range []int{4, 3} {
		st.frame()
		u.btn("viewall:" + c.ID).Click()
		st.frame()
		st.frame()
		if shown(entryViewAll) != 1 || shown(entryGroup) != want {
			t.Errorf("unfolding: %d View all, %d rows, want %d", shown(entryViewAll), shown(entryGroup), want)
		}
	}
	// Without its announcements the name isn't a button, and none at all
	// still draws.
	c.Announcements = ""
	st.frame()
	u.communities = nil
	st.frame()
}

// TestEmptyPaneActions presses the buttons of the Status and Channels
// pages with nothing open. "Photo and video" opens the system's dialog, so
// it's only drawn.
func TestEmptyPaneActions(t *testing.T) {
	st := newSlashTest(t, "rina")
	u := st.u
	press := func(key string) {
		st.frame()
		u.btn(key).Click()
		st.frame()
		st.frame()
	}
	u.setPage(pageStatus)
	press("pane:st-text")
	if !u.status.text.isOpen() {
		t.Error("Text status didn't open the composer")
	}
	u.setPage(pageChannels)
	press("pane:ch-find")
	if !u.channel.discovering {
		t.Error("Explore channels asked for no suggestions")
	}
	press("pane:ch-create")
	if u.dialog.kind != dialogChannel {
		t.Error("Create channel didn't open its dialog")
	}

	// Following none.
	u.dialog = dialogState{}
	u.channels = nil
	press("channel-create")
	if u.dialog.kind != dialogChannel {
		t.Error("the list's Create channel didn't open its dialog")
	}
}

// TestTextStatus checks who the composer says it goes to, and its emoji
// button.
func TestTextStatus(t *testing.T) {
	for want, p := range map[string]*model.StatusPrivacy{
		"":                       nil,
		"Status (Contacts)":      {},
		"Status (2 excluded)":    {Audience: model.AudienceExcept, Count: 2},
		"Status (3 included)":    {Audience: model.AudienceOnly, Count: 3},
		"Status (Close friends)": {Audience: model.AudienceClose, Count: 4},
		"Status (Gym)":           {Audience: model.AudienceList, ListID: "1", Lists: []model.StatusList{{ID: "1", Name: "Gym"}}},
	} {
		if got := statusAudienceLabel(p); got != want {
			t.Errorf("%+v: %q, want %q", p, got, want)
		}
	}

	st := newSlashTest(t, "rina")
	u := st.u
	u.setPage(pageStatus)
	u.openStatusText()
	st.frame()
	if want := statusAudienceLabel(st.b.StatusPrivacy()); want == "" || u.status.text.audience != want {
		t.Errorf("goes to %q, want %q", u.status.text.audience, want)
	}
	st.b.SetStatusPrivacy(model.StatusPrivacy{})
	u.applyEvents()
	if got := u.status.text.audience; got != "Status (Contacts)" {
		t.Errorf("goes to %q once that changed", got)
	}
	openPicker := func() {
		u.btn("stt:emoji").Click()
		st.frame()
		st.frame()
		if !u.picker.open || u.picker.mode != pickStatus {
			t.Fatal("the emoji button opened no picker")
		}
	}
	openPicker()
	u.pickEmoji("🌊")
	st.frame()
	st.frame()
	if got := u.status.text.ed.Text(); got != "🌊" || u.picker.open {
		t.Errorf("picked %q, picker open: %v", got, u.picker.open)
	}
	// Esc closes the picker, then the composer.
	openPicker()
	st.press(key.NameEscape)
	if u.picker.open || !u.status.text.isOpen() {
		t.Errorf("Esc: picker open %v, composer open %v", u.picker.open, u.status.text.isOpen())
	}
	u.Escape()
	if u.status.text.isOpen() {
		t.Error("the second Esc left the composer open")
	}
}
