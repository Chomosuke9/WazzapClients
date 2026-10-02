package ui

import (
	"strings"
	"testing"

	"gioui.org/io/key"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestOpenFocusesComposer checks that opening a chat puts the caret in its
// composer, ready to type, as WhatsApp does.
func TestOpenFocusesComposer(t *testing.T) {
	st := newSlashTest(t, "rina")
	st.frame()
	ed := &st.u.conv.composer
	if !st.r.Source().Focused(ed) {
		t.Fatal("the composer isn't focused after opening a chat")
	}
	st.u.requestFocus(nil) // something else took the keys
	st.frame()
	st.u.SelectID("work")
	st.frame()
	st.frame()
	if !st.r.Source().Focused(ed) {
		t.Fatal("the composer isn't focused after switching chats")
	}
}

// TestMentionKeys checks that the arrow keys move through the mention
// picker, and that Enter and Tab pick the highlighted member.
func TestMentionKeys(t *testing.T) {
	st := newSlashTest(t, "work")
	st.typeText("@")
	ms := st.u.mentionQuery()
	if ms == nil || len(ms.members) < 3 {
		t.Fatalf("mention picker for @: %+v", ms)
	}
	picked := func(m model.Member) string {
		if m.Me {
			return "@" + st.u.meName() + " "
		}
		return "@" + strings.TrimPrefix(m.Name, "~") + " "
	}

	st.press(key.NameDownArrow)
	st.press(key.NameDownArrow)
	st.press(key.NameReturn)
	if want := picked(ms.members[2]); st.text() != want {
		t.Fatalf("Down, Down, Enter picked %q, want %q", st.text(), want)
	}

	// Up from the first row goes round to the last; Tab picks too.
	st.typeText("@")
	st.press(key.NameUpArrow)
	st.press(key.NameTab)
	if want := picked(ms.members[len(ms.members)-1]); st.text() != want {
		t.Fatalf("Up, Tab picked %q, want %q", st.text(), want)
	}

	// A new query starts on the first row again.
	st.typeText("@")
	st.press(key.NameDownArrow)
	st.typeText("@a") // "@all" still matches
	if st.u.mentionQuery() == nil || st.u.conv.mentionSel != 0 {
		t.Fatalf("highlight on row %d after typing, want 0", st.u.conv.mentionSel)
	}

	// Enter picks even when it adds a line rather than sending.
	st.u.conv.composer.Submit = false
	st.typeText("@")
	st.press(key.NameReturn)
	if want := picked(ms.members[0]); st.text() != want {
		t.Fatalf("Enter with Ctrl+Enter sending picked %q, want %q", st.text(), want)
	}
}

// TestMentionPickerNames checks that the mention picker finds members you
// haven't saved by their number typed any way, names them by the name they
// gave themselves, and ranks better matches first.
func TestMentionPickerNames(t *testing.T) {
	st := newSlashTest(t, "work")
	u := st.u
	u.conv.members, u.conv.membersFor = &model.ChatInfo{ID: "work", IsGroup: true, Members: []model.Member{
		{ID: "me", Name: "You", Me: true},
		{ID: "hasan@lid", Name: "Hasan", Contact: "Hasan", Phone: "+62 811-0000-1111"},
		{ID: "budi@lid", Name: "Budi Santoso", Contact: "Budi Santoso", Phone: "+62 812-3456-7890"},
		{ID: "naufal@lid", Name: "+62 857-1111-2222", Push: "naufalll", Phone: "+62 857-1111-2222"},
	}}, "work"
	u.conv.hitsOK = false

	st.typeText("@0857")
	ms := u.mentionQuery()
	if ms == nil || len(ms.members) != 1 || ms.members[0].ID != "naufal@lid" {
		t.Fatalf("@0857 found %+v, want only naufal", ms)
	}
	st.press(key.NameReturn)
	if st.text() != "@naufalll " {
		t.Fatalf("picked %q, want the push name", st.text())
	}
	if d := u.draftFrom(trimSpace(st.text())); d.Text != "@naufal" || len(d.Mentions) != 1 || d.Mentions[0] != "naufal@lid" {
		t.Fatalf("draft %q mentions %v", d.Text, d.Mentions)
	}

	// A word's start beats the same letters inside a name.
	st.typeText("@san")
	if ms := u.mentionQuery(); ms == nil || len(ms.members) != 2 || ms.members[0].ID != "budi@lid" {
		t.Fatalf("@san found %+v, want Budi Santoso, then Hasan", ms)
	}

	// The slash commands' member picker finds and shows them the same way.
	st.typeText("/kick @2222")
	sp := u.slashQuery()
	if sp == nil || len(sp.vals) != 1 || sp.vals[0].id != "naufal@lid" ||
		sp.vals[0].title != "~naufalll" || sp.vals[0].sub != "+62 857-1111-2222" {
		t.Fatalf("/kick @2222 offers %+v", sp)
	}
}
