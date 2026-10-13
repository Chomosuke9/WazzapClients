package ui

import (
	"image"
	"slices"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// chatListUI is a demo window with frame and click helpers.
func chatListUI(t *testing.T) (*UI, *mock.Backend, func(), func(string)) {
	t.Helper()
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	now := testNow()
	var ops op.Ops
	var r input.Router
	frame := func() {
		ops.Reset()
		u.Layout(layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1600, 900))})
		r.Frame(&ops)
		now = now.Add(10 * time.Millisecond)
	}
	click := func(key string) {
		u.btn(key).Click()
		frame()
		frame()
	}
	frame()
	return u, b, frame, click
}

func visibleIDs(u *UI) []string {
	var ids []string
	for _, c := range u.sidebar.visible {
		ids = append(ids, c.ID)
	}
	return ids
}

// TestListChips checks that each custom list has a chip that shows its
// chats, and that the "+" chip makes a new list.
func TestListChips(t *testing.T) {
	u, b, frame, click := chatListUI(t)
	items := u.chipItems()
	var names []string
	for _, it := range items {
		names = append(names, it.name)
	}
	if want := []string{"All", "Unread", "Favourites", "Groups", "Family", "Work", "New list"}; !slices.Equal(names, want) {
		t.Fatalf("chips %q, want %q", names, want)
	}
	click("chip:4") // Family
	if got := visibleIDs(u); !slices.Equal(got, []string{"family", "mom"}) && !slices.Equal(got, []string{"mom", "family"}) {
		t.Errorf("Family shows %v", got)
	}

	click("chip:6") // +
	if u.dialog.kind != dialogForward || !u.dialog.newList {
		t.Fatal("the + chip didn't open New list")
	}
	u.dialog.listName.SetText("Gym")
	click("fwd:gym")
	frame()
	click("fwd:send")
	for range 30 {
		frame()
	}
	ls := b.Lists()
	if l := ls[len(ls)-1]; l.Name != "Gym" || !slices.Equal(l.Chats, []string{"gym"}) {
		t.Fatalf("new list %q with %v", l.Name, l.Chats)
	}
	if n := len(u.chipItems()); n != 8 {
		t.Errorf("%d chips after the new list, want 8", n)
	}
}

// TestListSearch checks that the chat list's search finds archived chats,
// contacts you have no chat with and messages.
func TestListSearch(t *testing.T) {
	u, b, frame, _ := chatListUI(t)
	search := func(q string) []findRow {
		u.sidebar.search.SetText(q)
		for range 3 {
			frame()
		}
		return u.sidebar.find.rows
	}
	rows := search("old project")
	if len(rows) < 2 || rows[0].heading != "Chats" || rows[1].chat == nil || rows[1].chat.ID != "old-project" {
		t.Errorf("an archived chat isn't found: %+v", rows)
	}

	var contact *model.Contact
	for _, c := range b.Contacts() {
		if u.chatByID(c.ID) == nil {
			contact = c
			break
		}
	}
	if contact == nil {
		t.Fatal("the demo has no contact without a chat")
	}
	found := false
	for _, r := range search(contact.Name) {
		found = found || r.contact != nil && r.contact.ID == contact.ID
	}
	if !found {
		t.Errorf("contact %s isn't found", contact.Name)
	}

	var heading bool
	var msgs int
	for _, r := range search("staging freeze") {
		heading = heading || r.heading == "Messages"
		if r.msg != nil {
			msgs++
		}
	}
	if !heading || msgs == 0 {
		t.Errorf("messages aren't found (heading %v, %d messages)", heading, msgs)
	}
}
