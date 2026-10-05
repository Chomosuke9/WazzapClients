package ui

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

func waitSnippetUI(t *testing.T, st *slashTest, done func() bool) {
	t.Helper()
	deadline := testNow().Add(5 * time.Second)
	for !done() && testNow().Before(deadline) {
		st.frame()
		runtime.Gosched()
	}
	if !done() {
		t.Fatal("snippet UI did not finish")
	}
}

func TestSnippetSaveSettingsAndPicker(t *testing.T) {
	st := newSlashTest(t, "rina")
	u := st.u
	original := st.b.Messages("rina", 1)[0]
	u.conv.reply = original
	st.typeText("/snippet save")
	st.press(key.NameReturn)
	waitSnippetUI(t, st, func() bool { notes := u.slash.notes["rina"]; return len(notes) == 1 && !notes[0].note.Busy })
	items, err := st.b.Snippets()
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	if items[0].Name != "Snippet 1" {
		t.Fatal(items[0].Name)
	}
	u.openSnippetSettings(items[0].ID)
	waitSnippetUI(t, st, func() bool { return u.settings.snippets != nil && !u.settings.snippets.loading })
	s := u.settings.snippets
	s.name.SetText("Greeting")
	s.body.SetText("Hello {name}\nSecond line")
	u.saveSnippetSettings()
	waitSnippetUI(t, st, func() bool { return !s.busy })
	saved, _ := st.b.Snippet(items[0].ID)
	if saved.Name != "Greeting" || saved.Body != "Hello {name}\nSecond line" {
		t.Fatal(saved)
	}
	u.settingsBack()
	waitSnippetUI(t, st, func() bool { return !u.settings.snippets.loading })
	u.settings.snippets.search.SetText("not present")
	st.frame()
	for _, sec := range u.settingsRows() {
		for _, row := range sec.rows {
			if strings.HasPrefix(row.key, "snippet:item:") {
				t.Fatal("search didn't filter")
			}
		}
	}
	u.setPage(pageChats)
	u.SelectID("rina")
	u.conv.reply = original
	// /snippet offers send and save, and send the saved snippets.
	st.typeText("/Snippet ")
	if sp := u.slashQuery(); sp == nil || len(sp.vals) != 2 || sp.vals[0].name != "send" || sp.vals[1].name != "save" {
		t.Fatalf("offered %+v", sp)
	}
	st.press(key.NameReturn) // picks send
	if st.text() != "/Snippet send " {
		t.Fatalf("picked %q", st.text())
	}
	waitSnippetUI(t, st, func() bool { sp := u.slashQuery(); return sp != nil && len(sp.vals) == 1 })
	if sp := u.slashQuery(); sp.vals[0].name != "Greeting" {
		t.Fatalf("offered %+v", sp.vals)
	}
	st.typeText("/Snippet send gre")
	st.press(key.NameReturn) // picks
	if st.text() != "/Snippet send Greeting " {
		t.Fatalf("picked %q", st.text())
	}
	before := len(st.b.Messages("rina", 1000))
	st.press(key.NameReturn) // sends
	waitSnippetUI(t, st, func() bool { return len(st.b.Messages("rina", 1000)) > before })
	if u.dialog.isOpen() || st.text() != "" {
		t.Fatal("sending opened a dialog or kept the command")
	}
	last := st.b.Messages("rina", 1)[0]
	// The picker fills {name} with the open chat's contact.
	if last.Text != "Hello Rina Kartika\nSecond line" || last.Quote == nil || last.Quote.ID != original.ID {
		t.Fatalf("sent snippet: %+v", last)
	}
}

func TestCatchLocalOnlyAndMissingReply(t *testing.T) {
	st := newSlashTest(t, "rina")
	before := len(st.b.Messages("rina", 1000))
	st.typeText("/catch ")
	st.press(key.NameReturn)
	if st.u.dialog.kind == dialogPayload {
		t.Fatal("catch without reply opened payload")
	}
	st.u.conv.reply = st.b.Messages("rina", 1)[0]
	st.typeText("/catch ")
	st.press(key.NameReturn)
	waitSnippetUI(t, st, func() bool { return st.u.dialog.snippet != nil && !st.u.dialog.snippet.loading })
	d := st.u.dialog.snippet
	if st.u.dialog.kind != dialogPayload || !strings.Contains(d.body.Text(), "conversation") {
		t.Fatal("missing payload")
	}
	st.u.btn("snip:copy").Click()
	st.frame()
	if len(st.b.Messages("rina", 1000)) != before {
		t.Fatal("catch sent a message to WhatsApp")
	}
}

func TestSnippetSaveNamedAndUnknown(t *testing.T) {
	st := newSlashTest(t, "rina")
	u := st.u
	u.conv.reply = st.b.Messages("rina", 1)[0]
	st.typeText("/snippet save Thanks a lot")
	st.press(key.NameReturn)
	waitSnippetUI(t, st, func() bool { n := st.lastNote("rina"); return n != nil && !n.Busy })
	if items, _ := st.b.Snippets(); len(items) != 1 || items[0].Name != "Thanks a lot" {
		t.Fatalf("saved %+v", items)
	}
	// The picker loads the new one again, after send only.
	st.typeText("/snippet save thanks")
	if sp := u.slashQuery(); sp == nil || len(sp.vals) != 0 {
		t.Fatal("save offered snippets")
	}
	st.typeText("/snippet send thanks")
	waitSnippetUI(t, st, func() bool { sp := u.slashQuery(); return sp != nil && len(sp.vals) == 1 })
	before := len(st.b.Messages("rina", 1000))
	st.typeText("/snippet send nothing like it")
	st.press(key.NameReturn)
	waitSnippetUI(t, st, func() bool { n := st.lastNote("rina"); return n != nil && n.Failed })
	if len(st.b.Messages("rina", 1000)) != before {
		t.Fatal("an unknown snippet sent something")
	}
}

func TestSnippetAccountStateAndClosedAsyncDialog(t *testing.T) {
	st := newSlashTest(t, "rina")
	st.u.openPayload("rina", st.b.Messages("rina", 1)[0].ID)
	old := st.u.dialog.snippet
	st.u.openPayload("rina", st.b.Messages("rina", 1)[0].ID)
	waitSnippetUI(t, st, func() bool { return !st.u.dialog.snippet.loading })
	if st.u.dialog.snippet == old {
		t.Fatal("old request reopened its dialog")
	}
	st.u.openSnippetSettings(0)
	st.u.openSettings(settingGeneral)
	st.frame()
	if st.u.settings.snippets != nil {
		t.Fatal("settings retained payload editor")
	}
}

// A poll or a document saves whole, not only its text, and sends whole.
func TestSnippetSavesWholeMessage(t *testing.T) {
	st := newSlashTest(t, "rina")
	u := st.u
	find := func(ok func(m *model.Message) bool) *model.Message {
		for _, c := range st.b.Chats() {
			for _, m := range st.b.Messages(c.ID, 1000) {
				if ok(m) {
					return m
				}
			}
		}
		t.Fatal("no such demo message")
		return nil
	}
	poll := find(func(m *model.Message) bool { return m.Poll != nil })
	doc := find(func(m *model.Message) bool { return m.Media == model.MediaDocument })
	for name, src := range map[string]*model.Message{"Poll": poll, "Doc": doc} {
		u.conv.reply = src
		st.typeText("/snippet save " + name)
		st.press(key.NameReturn)
		waitSnippetUI(t, st, func() bool { n := st.lastNote("rina"); return n != nil && !n.Busy })
		if n := st.lastNote("rina"); n.Failed {
			t.Fatal(n.Text)
		}
		st.typeText("/snippet send " + name)
		st.press(key.NameReturn)
		waitSnippetUI(t, st, func() bool { m := st.b.Messages("rina", 1)[0]; return m.FromMe && m.Media == src.Media })
		got := st.b.Messages("rina", 1)[0]
		switch name {
		case "Poll":
			if got.Poll == nil || len(got.Poll.Options) != len(src.Poll.Options) || got.Text != src.Text {
				t.Fatalf("poll sent as %+v", got)
			}
		case "Doc":
			if got.FileName != src.FileName || got.FileSize != src.FileSize {
				t.Fatalf("document sent as %+v", got)
			}
		}
	}
	for _, s := range []string{"Poll", "Doc"} {
		items, _ := st.b.Snippets()
		for _, it := range items {
			if it.Name == s && !it.Payload {
				t.Fatalf("%s saved as text", s)
			}
		}
	}
}
