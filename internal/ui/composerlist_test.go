package ui

import (
	"testing"

	"gioui.org/io/key"
)

// TestComposerList checks that Shift+Enter continues a list in the
// composer, ends it on an empty item, and that Ctrl+Backspace deletes a
// word.
func TestComposerList(t *testing.T) {
	st := newSlashTest(t, "work")
	ed := &st.u.conv.composer
	shiftEnter := func() {
		st.r.Queue(key.Event{Name: key.NameReturn, Modifiers: key.ModShift, State: key.Press})
		st.frame()
	}
	for _, c := range []struct{ text, want string }{
		{"- milk", "- milk\n- "},
		{"* milk", "* milk\n* "},
		{"9. milk", "9. milk\n10. "},
		{"- milk\n- ", "- milk\n"},
		{"milk", "milk\n"},
		{"```\n- milk", "```\n- milk\n"},
	} {
		st.typeText(c.text)
		shiftEnter()
		if got := ed.Text(); got != c.want {
			t.Errorf("Shift+Enter after %q: %q, want %q", c.text, got, c.want)
		}
	}
	st.typeText("hello big world")
	st.r.Queue(key.Event{Name: key.NameDeleteBackward, Modifiers: key.ModCtrl, State: key.Press})
	st.frame()
	if got := ed.Text(); got != "hello big " {
		t.Errorf("Ctrl+Backspace left %q", got)
	}
}

// TestEscapeClosesSearchThenChat checks Esc's last two steps.
func TestEscapeClosesSearchThenChat(t *testing.T) {
	st := newSlashTest(t, "work")
	st.u.sidebar.search.SetText("an")
	st.frame()
	st.press(key.NameEscape)
	if st.u.sidebar.search.Text() != "" || st.u.selected == nil {
		t.Fatalf("Esc should close the search alone: query %q, chat %v", st.u.sidebar.search.Text(), st.u.selected)
	}
	st.press(key.NameEscape)
	if st.u.selected != nil {
		t.Fatal("Esc should close the chat")
	}
}
