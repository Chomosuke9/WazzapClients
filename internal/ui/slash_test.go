package ui

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/command"
	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// slashTest drives a UI with key events, like a window would.
type slashTest struct {
	t   *testing.T
	b   *mock.Backend
	u   *UI
	ops op.Ops
	r   input.Router
	now time.Time
}

func newSlashTest(t *testing.T, chat string) *slashTest {
	st := &slashTest{t: t, b: mock.New(), now: testNow()}
	st.b.SetPref(prefSlash, "on") // an extra feature, off by default
	st.u = New(st.b)
	st.u.Start(func() {})
	st.u.applyEvents()
	st.u.SelectID(chat)
	st.frame()
	return st
}

func (st *slashTest) frame() {
	st.ops.Reset()
	st.u.Layout(layout.Context{Ops: &st.ops, Now: st.now, Source: st.r.Source(),
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1100, 700))})
	st.r.Frame(&st.ops)
	st.now = st.now.Add(10 * time.Millisecond)
}

// typeText puts s in the composer with the caret at its end.
func (st *slashTest) typeText(s string) {
	ed := &st.u.conv.composer
	ed.SetText(s)
	ed.SetCaret(ed.Len(), ed.Len())
	st.u.requestFocus(ed)
	st.frame()
	st.frame()
}

func (st *slashTest) press(name key.Name) {
	st.r.Queue(key.Event{Name: name, State: key.Press})
	st.frame()
	st.frame()
}

func (st *slashTest) text() string { return st.u.conv.composer.Text() }

func TestSlashKick(t *testing.T) {
	st := newSlashTest(t, "work")
	st.typeText("/")
	if sp := st.u.slashQuery(); sp == nil || !sp.in.Naming || len(sp.cmds) < 2 {
		t.Fatalf("no command picker for /: %+v", sp)
	}
	// The second command is /kick.
	st.press(key.NameDownArrow)
	st.press(key.NameReturn)
	if st.text() != "/kick " {
		t.Fatalf("picked %q, want /kick", st.text())
	}
	// Enter picks the first member while none is given.
	st.press(key.NameReturn)
	if !strings.HasPrefix(st.text(), "/kick @") {
		t.Fatalf("picked %q, want a member", st.text())
	}
	name := strings.TrimSpace(strings.TrimPrefix(st.text(), "/kick @"))
	// Now Enter runs it.
	st.press(key.NameReturn)
	st.u.applyEvents()
	st.frame()
	if st.text() != "" {
		t.Fatalf("composer still has %q", st.text())
	}
	notes := st.u.slash.notes["work"]
	if len(notes) != 1 {
		t.Fatalf("%d notes, want 1", len(notes))
	}
	n := notes[0].note
	if n.Busy || n.Failed || n.Text != "Removed "+name+" from the group." {
		t.Fatalf("note %+v", n)
	}
	for _, m := range st.b.Info("work").Members {
		if m.Name == name {
			t.Fatalf("%s is still a member", name)
		}
	}
	// The note shows in the chat, and Dismiss removes it.
	found := false
	for _, r := range st.u.rows(st.u.selected) {
		found = found || r.kind == rowNote
	}
	if !found {
		t.Fatal("the note isn't in the chat")
	}
	st.u.dismissNote("work", n)
	if len(st.u.slash.notes["work"]) != 0 {
		t.Fatal("the note wasn't dismissed")
	}
}

func TestSlashSendsText(t *testing.T) {
	st := newSlashTest(t, "work")
	sent := func(s string) bool {
		for _, m := range st.u.msgs {
			if m.FromMe && m.Text == s {
				return true
			}
		}
		return false
	}
	// Not a command: it's sent as it is.
	st.typeText("/shrug that's life")
	st.press(key.NameReturn)
	if !sent("/shrug that's life") {
		t.Error("unknown command wasn't sent as text")
	}
	// A group command in a one-to-one chat is just text too.
	st.u.SelectID("rina")
	st.frame()
	st.typeText("/link ")
	if st.u.slashQuery() != nil {
		t.Error("/link offered in a one-to-one chat")
	}
	// With slash commands off, nothing is a command.
	st.u.SelectID("work")
	st.frame()
	st.u.slash.on = false
	st.typeText("/link ")
	st.press(key.NameReturn)
	if !sent("/link") || len(st.u.slash.notes["work"]) != 0 {
		t.Error("with commands off, /link wasn't sent as text")
	}
}

func TestSlashProblem(t *testing.T) {
	st := newSlashTest(t, "work")
	st.typeText("/description ")
	st.press(key.NameReturn)
	if st.u.slash.problem == "" || st.text() != "/description " {
		t.Fatalf("missing text ran: problem %q, text %q", st.u.slash.problem, st.text())
	}
	st.typeText("/description Release on Friday")
	st.press(key.NameReturn)
	st.u.applyEvents()
	if got := st.b.Info("work").About; got != "Release on Friday" {
		t.Fatalf("description is %q", got)
	}
}

func TestSlashLockdownChoice(t *testing.T) {
	st := newSlashTest(t, "work")
	// A whole choice runs at once.
	st.typeText("/lockdown on")
	st.press(key.NameReturn)
	st.u.applyEvents()
	if !st.b.Info("work").Announce {
		t.Fatal("/lockdown on didn't lock the group")
	}
	// Without a mode it switches.
	st.typeText("/lockdown ")
	st.press(key.NameReturn)
	st.u.applyEvents()
	if st.b.Info("work").Announce {
		t.Fatal("/lockdown didn't unlock the group")
	}
}

func TestSlashEscape(t *testing.T) {
	st := newSlashTest(t, "work")
	st.typeText("/")
	if !st.u.slashShown(st.u.slashQuery()) {
		t.Fatal("picker not shown")
	}
	st.press(key.NameEscape)
	if st.u.slashShown(st.u.slashQuery()) {
		t.Fatal("Esc didn't close the picker")
	}
	st.typeText("/k")
	if !st.u.slashShown(st.u.slashQuery()) {
		t.Fatal("picker didn't come back after typing")
	}
}

// TestExtrasOffByDefault checks that extra features start off, and that
// the Extra features page turns them on.
func TestExtrasOffByDefault(t *testing.T) {
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	u.applyEvents()
	if u.slash.on || u.adminMention || u.rawPhotos {
		t.Fatalf("extras on by default: slash %v, @admin %v, raw %v", u.slash.on, u.adminMention, u.rawPhotos)
	}
	u.SelectID("work")
	hasAdmin := func() bool {
		ed := &u.conv.composer
		ed.SetText("@")
		ed.SetCaret(1, 1)
		u.slash.cacheOK = false
		ms := u.mentionQuery()
		if ms == nil {
			t.Fatal("no mention picker for @")
		}
		for _, m := range ms.members {
			if m.ID == mentionAdminID {
				return true
			}
		}
		return false
	}
	if hasAdmin() {
		t.Error("@admin offered while off")
	}
	u.attach.files = []*attachFile{{Attachment: model.Attachment{Path: "a.jpg", Media: model.MediaImage}}}
	hasRaw := func() bool {
		for _, it := range u.qualityMenuItems() {
			if it.key == "raw" {
				return true
			}
		}
		return false
	}
	if hasRaw() {
		t.Error("Raw quality offered while off")
	}

	u.ShowPage("extras")
	for _, key := range []string{prefSlash, prefAdminMention, prefRawPhotos} {
		settingRowByKey(t, u, key).run()
		u.settings.stale = true
		if b.Pref(key) != "on" || !settingRowByKey(t, u, key).on {
			t.Errorf("%s didn't turn on", key)
		}
	}
	if !u.slash.on || !hasAdmin() || !hasRaw() {
		t.Error("extras turned on aren't offered")
	}
	// A new window remembers them.
	if u2 := New(b); !u2.slash.on || !u2.adminMention || !u2.rawPhotos {
		t.Error("extras weren't remembered")
	}
	// Raw goes when it's turned off.
	u.attach.quality = model.QualityRaw
	settingRowByKey(t, u, prefRawPhotos).run()
	if u.attach.quality != model.QualityHD || hasRaw() {
		t.Error("Raw quality stayed after turning it off")
	}
}

// TestSlashStickerHint checks the composer's hint for /sticker's two
// texts, which # separates.
func TestSlashStickerHint(t *testing.T) {
	st := newSlashTest(t, "rina")
	for _, c := range []struct{ text, hint string }{
		{"/sticker ", "[top] #[bottom]"},
		{"/sticker when it works", "#[bottom]"},
		{"/sticker when it works#", ""},
	} {
		st.typeText(c.text)
		sp := st.u.slashQuery()
		if sp == nil {
			t.Fatalf("%q isn't a command", c.text)
		}
		if got := st.u.slashHint(sp); got != c.hint {
			t.Errorf("%q: hint %q, want %q", c.text, got, c.hint)
		}
	}
}

// TestNoteBeforeMessages checks that a note older than every loaded
// message still shows when the chat's start is loaded: the demo chat's
// messages are from 09:00 today, so a test run early in the day used to
// lose its note.
func TestNoteBeforeMessages(t *testing.T) {
	st := newSlashTest(t, "work")
	u := st.u
	y, m, d := testNow().Date()
	u.now = func() time.Time { return time.Date(y, m, d, 5, 0, 0, 0, time.UTC) }
	slashHost{u: u, chat: "work"}.Note(&command.Note{Text: "Done."})
	hasNote := func() bool {
		for _, r := range u.rows(u.selected) {
			if r.kind == rowNote {
				return true
			}
		}
		return false
	}
	if !hasNote() {
		t.Fatal("a note older than the chat's messages isn't shown")
	}
	// It belongs to an older page while that isn't loaded.
	u.conv.olderMore = true
	u.msgsVer++
	if hasNote() {
		t.Error("a note older than the loaded page is shown")
	}
}
