package ui

import (
	"testing"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
)

// TestShortcodeQuery checks when ":query" opens the picker.
func TestShortcodeQuery(t *testing.T) {
	st := newSlashTest(t, "rina")
	for txt, want := range map[string]bool{
		":mo":        true,
		"hi :moa":    true,
		"🙂:moa":      true,
		":m":         false, // two letters at least
		"at 10:30":   false,
		"http://moa": false,
		"x:moa":      false,
		":zzzzqq":    false, // nothing matches
		":moai ":     false,
	} {
		st.typeText(txt)
		if got := st.u.shortcodeQuery() != nil; got != want {
			t.Errorf("%q: picker shown %v, want %v", txt, got, want)
		}
	}
	st.typeText(":moa")
	if cs := st.u.shortcodeQuery(); cs == nil || cs.hits[0].char != "🗿" {
		t.Fatalf(":moa offers %+v, want 🗿 first", cs)
	}
}

// TestShortcodePick checks that Enter picks the highlighted emoji, that
// Esc closes the picker, and that typing a whole ":name:" turns it into
// its emoji.
func TestShortcodePick(t *testing.T) {
	st := newSlashTest(t, "rina")
	st.typeText("look :moa")
	st.press(key.NameReturn)
	if st.text() != "look 🗿" {
		t.Fatalf("Enter on :moa gave %q, want look 🗿", st.text())
	}
	if st.u.picker.recent[0] != "🗿" {
		t.Errorf("recent emojis %v don't start with 🗿", st.u.picker.recent)
	}

	st.typeText(":thumbs")
	st.press(key.NameEscape)
	if st.u.shortcodeQuery() != nil {
		t.Fatal("Esc didn't close the picker")
	}
	st.typeText(":thumbsu")
	if st.u.shortcodeQuery() == nil {
		t.Fatal("the picker stayed closed after typing on")
	}

	// Typing the closing colon completes it, in the picker's skin tone.
	st.u.picker.tone = 2
	st.typeText("ok :thumbsup")
	typeColon(st)
	if want := "ok " + withTone("👍", 2); st.text() != want {
		t.Fatalf("typing :thumbsup: gave %q, want %q", st.text(), want)
	}
	// Not for a name that isn't one, or a colon typed elsewhere.
	st.typeText(":notanemoji")
	typeColon(st)
	if st.text() != ":notanemoji:" {
		t.Fatalf("an unknown name became %q", st.text())
	}
}

// TestToneMenuAtEmoji checks that a right click on an emoji in the picker
// over the composer opens its tones above it, where the click was: the
// picker is drawn in the conversation's coordinates, not the window's.
func TestToneMenuAtEmoji(t *testing.T) {
	st := newSlashTest(t, "rina")
	st.u.openPicker(pickComposer, nil)
	st.u.picker.search.SetText("hand") // hands, which take tones
	for range 30 {
		st.frame()
	}
	click := func(p f32.Point, b pointer.Buttons) {
		st.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: p, Buttons: b},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p})
		st.frame()
		st.frame()
	}
	var at f32.Point
	m := &st.u.picker.tones
	for y := float32(150); y < 650 && !m.open; y += 16 {
		for x := float32(700); x < 1100 && !m.open; x += 16 {
			at = f32.Pt(x, y)
			click(at, pointer.ButtonSecondary)
		}
	}
	if !m.open || m.of == "" {
		t.Fatal("no right click opened an emoji's tones")
	}
	for range 30 {
		st.frame()
	}
	// The menu is centered above the emoji: its middle tone (the fourth of
	// six) is 19 dp right of the click and 56 dp above it.
	click(at.Add(f32.Pt(19, -56)), pointer.ButtonPrimary)
	if want := withTone(m.of, 3); st.text() != want {
		t.Fatalf("clicking above a right-clicked %s put %q in the composer, want %q", m.of, st.text(), want)
	}
}

// typeColon types ":" at the caret, as the keyboard would.
func typeColon(st *slashTest) {
	c, _ := st.u.conv.composer.Selection()
	st.r.Queue(key.EditEvent{Range: key.Range{Start: c, End: c}, Text: ":"})
	st.frame()
	st.frame()
}
