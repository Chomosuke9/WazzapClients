package ui

import (
	"testing"

	"gioui.org/io/key"

	"github.com/chomosuke9/wazzapclients/internal/command"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// lastNote returns the newest note in chat.
func (st *slashTest) lastNote(chat string) *command.Note {
	st.t.Helper()
	ns := st.u.slash.notes[chat]
	if len(ns) == 0 {
		st.t.Fatal("no note")
	}
	return ns[len(ns)-1].note
}

func (st *slashTest) unread(chat string) int {
	for _, c := range st.b.Chats() {
		if c.ID == chat {
			return c.Unread
		}
	}
	return -1
}

// TestGrayCommandOff checks that a gray command is neither offered nor
// run until its own switch is on.
func TestGrayCommandOff(t *testing.T) {
	st := newSlashTest(t, "work")
	st.typeText("/gh")
	if sp := st.u.slashQuery(); sp != nil {
		t.Fatalf("/ghost offered while its switch is off: %+v", sp.cmds)
	}
	st.typeText("/ghost ")
	if sp := st.u.slashQuery(); sp != nil {
		t.Fatal("/ghost read as a command while its switch is off")
	}
	st.b.SetPref(grayCmdPref("ghost"), "on")
	st.u.loadExtras()
	st.u.slash.cacheOK = false
	st.typeText("/gh")
	if sp := st.u.slashQuery(); sp == nil || len(sp.cmds) != 1 || sp.cmds[0].Name != "ghost" {
		t.Fatal("/ghost not offered with its switch on")
	}
}

func TestSlashGhost(t *testing.T) {
	st := newSlashTest(t, "work")
	st.b.SetPref(grayCmdPref("ghost"), "on") // a gray command, off by default
	st.u.loadExtras()
	st.typeText("/ghost ")
	st.press(key.NameReturn)
	if !st.u.ghostMode() || st.b.Pref(model.PrefGhost) != "on" {
		t.Fatal("/ghost didn't turn ghost mode on")
	}
	if n := st.lastNote("work"); n.Text != command.GhostOnText {
		t.Fatalf("note %q", n.Text)
	}
	// Opening a chat leaves it unread for the backend: no read receipts.
	st.u.SelectID("gym")
	st.frame()
	if n := st.unread("gym"); n != 27 {
		t.Fatalf("gym has %d unread in ghost mode, want 27", n)
	}
	// Nothing goes from the composer, or from files.
	before := len(st.b.Messages("gym", 1000))
	st.typeText("hello")
	st.press(key.NameReturn)
	st.u.addFiles("gym", []*attachFile{{Attachment: model.Attachment{Path: "beach.jpg", Media: model.MediaImage}}})
	if len(st.b.Messages("gym", 1000)) != before || len(st.u.attach.files) != 0 {
		t.Fatal("sent in ghost mode")
	}
	// It stays on in a new window.
	if !New(st.b).ghostMode() {
		t.Fatal("ghost mode didn't stay on")
	}
	// The button in the composer's place turns it off and marks the open
	// chat read.
	st.u.btn("ghost:compose").Click()
	st.frame()
	if st.u.ghostMode() || st.b.Pref(model.PrefGhost) != "off" {
		t.Fatal("Turn off didn't turn ghost mode off")
	}
	if n := st.unread("gym"); n != 0 {
		t.Fatalf("gym has %d unread after ghost mode, want 0", n)
	}
	st.typeText("hello")
	st.press(key.NameReturn)
	if len(st.b.Messages("gym", 1000)) != before+1 {
		t.Fatal("can't send after ghost mode")
	}
}

func TestGhostBar(t *testing.T) {
	st := newSlashTest(t, "work")
	st.u.setGhost(true)
	st.frame()
	st.u.btn("ghost:off").Click()
	st.frame()
	if st.u.ghostMode() || st.b.Pref(model.PrefGhost) != "off" {
		t.Fatal("the bar's Turn off didn't turn ghost mode off")
	}
}
