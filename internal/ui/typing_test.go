package ui

import (
	"slices"
	"testing"

	"gioui.org/io/key"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

type typingBackend struct {
	model.Backend
	reports []string
}

func (b *typingBackend) ReportTyping(chat string) { b.reports = append(b.reports, chat) }

func TestComposerTypingPresence(t *testing.T) {
	st := newSlashTest(t, "rina")
	b := &typingBackend{Backend: st.u.backend}
	st.u.backend = b
	check := func(want ...string) {
		t.Helper()
		if !slices.Equal(b.reports, want) {
			t.Fatalf("reports = %v, want %v", b.reports, want)
		}
		b.reports = nil
	}
	st.typeText("saved draft")
	check() // programmatic restoration isn't activity
	chat := st.u.selected.ID
	st.r.Queue(key.EditEvent{Range: key.Range{Start: 11, End: 11}, Text: "!"})
	st.frame()
	check(chat)
	st.press(key.NameReturn)
	check("")
	st.r.Queue(key.EditEvent{Text: "hello"})
	st.frame()
	check(chat)
	st.u.SelectID("work")
	check("")
	st.u.SelectID("rina")
	st.frame()
	check() // draft loaded without announcing typing
	st.u.reportComposerTyping()
	check(chat)
	st.r.Queue(key.EditEvent{Range: key.Range{Start: 0, End: st.u.conv.composer.Len()}, Text: ""})
	st.frame()
	check("")
	st.typeText("draft")
	st.u.away = true
	st.u.reportComposerTyping()
	check()
	st.u.away = false
	st.u.setGhost(true)
	st.u.reportComposerTyping()
	check()
}
