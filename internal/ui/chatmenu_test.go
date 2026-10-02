package ui

import (
	"slices"
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestConvMenu checks the chat's ⋮ menu offers a group's and a contact's
// actions, and that its items do something.
func TestConvMenu(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	labels := func(id string) []string {
		u.SelectID(id)
		var out []string
		for _, it := range u.convMenuItems(u.selected) {
			if it.run == nil && !it.divider {
				t.Errorf("%s: %q does nothing", id, it.label)
			}
			out = append(out, it.label)
		}
		return out
	}
	group := labels("family")
	for _, want := range []string{"Add member", "Group info", "Search", "Select messages", "Disappearing messages",
		"Chat theme", "Add to list", "Export chat", "Close chat", "Clear chat", "Exit group"} {
		if !slices.Contains(group, want) {
			t.Errorf("group menu %q lacks %q", group, want)
		}
	}
	contact := labels("rina")
	for _, want := range []string{"Contact info", "Block", "Report", "Delete chat"} {
		if !slices.Contains(contact, want) {
			t.Errorf("contact menu %q lacks %q", contact, want)
		}
	}
	if slices.Contains(contact, "Add member") || slices.Contains(contact, "Exit group") {
		t.Errorf("contact menu %q has group actions", contact)
	}
}

// TestDisappearingAndTheme sets a chat's timer and theme.
func TestDisappearingAndTheme(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("family")
	var week *menuItem
	items := u.timerItems(u.selected)
	for i := range items {
		if items[i].label == "7 days" {
			week = &items[i]
		}
	}
	if week == nil {
		t.Fatal("no 7 days timer")
	}
	week.run()
	u.applyEvents()
	if info := u.backend.Info("family"); info.Disappearing != uint32(7*24*time.Hour/time.Second) {
		t.Errorf("timer = %d s, want 7 days", info.Disappearing)
	}
	u.setChatTheme("family", 2)
	u.themes = nil // read back from the prefs
	if got := u.chatThemeOf("family"); got != 2 {
		t.Errorf("theme = %d, want 2", got)
	}
	if u.chatPalette(u.selected).BubbleOut == u.pal.BubbleOut {
		t.Error("the theme didn't change your bubbles")
	}
}

// TestGalleryPanel opens the Media panel and switches its tabs.
func TestGalleryPanel(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.openGallery("", "")
	u.applyEvents()
	if len(u.gallery.list.msgs) == 0 {
		t.Fatal("no media from all chats")
	}
	for _, m := range u.gallery.list.msgs {
		if m.Kind != model.KindImage && m.Media != model.MediaImage && m.Media != model.MediaVideo {
			t.Errorf("media tab lists %+v", m)
		}
	}
	u.gallery.tab = model.GalleryDocs
	u.loadGallery()
	u.applyEvents()
	if len(u.gallery.list.msgs) == 0 {
		t.Fatal("no documents")
	}
	for _, m := range u.gallery.list.msgs {
		if m.Media != model.MediaDocument {
			t.Errorf("docs tab lists %+v", m)
		}
	}
	u.closeGallery()
	if u.gallery.open {
		t.Error("the panel stayed open")
	}
}
