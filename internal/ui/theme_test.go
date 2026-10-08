package ui

import (
	"strings"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/mock"
)

// The theme follows the system's unless Light or Dark was picked, and a
// system that doesn't say gets the dark one.
func TestSystemTheme(t *testing.T) {
	defer func(f func() (bool, bool)) { systemDark = f }(systemDark)
	sys, known := false, true
	systemDark = func() (bool, bool) { return sys, known }

	u := New(mock.New())
	if u.dark {
		t.Fatal("a light system should start light")
	}
	sys = true
	u.applyTheme() // the window got focus
	if !u.dark {
		t.Fatal("didn't follow the system to dark")
	}
	u.setTheme("light")
	if u.dark {
		t.Fatal("Light didn't override the system")
	}
	u.setTheme("")
	if !u.dark {
		t.Fatal("System default didn't go back to the system's")
	}
	known = false
	sys = false
	u.applyTheme()
	if !u.dark {
		t.Fatal("an unknown system should be dark")
	}
}

// The emoji button turns into the sticker smiley after the sticker tab,
// and the panel opens at that tab again.
func TestComposerPickerTab(t *testing.T) {
	u := New(mock.New())
	u.openPicker(pickComposer, nil)
	if u.picker.tab != tabEmoji {
		t.Fatal("the panel should first open at emoji")
	}
	u.picker.composerTab = tabSticker // as a click on the tab does
	u.closePicker()
	u.openPicker(pickComposer, nil)
	if u.picker.tab != tabSticker {
		t.Fatal("the panel didn't reopen at the sticker tab")
	}
	u.closePicker()
	u.openPicker(pickReaction, nil)
	if u.picker.tab != tabEmoji {
		t.Fatal("reactions should always open at emoji")
	}
}

// The voice message speed goes 1×, 1.5×, 2× and round, and is kept.
func TestVoiceRate(t *testing.T) {
	b := mock.New()
	u := New(b)
	var got []string
	for range 4 {
		u.nextVoiceRate()
		got = append(got, rateName(u.voiceRate))
	}
	if want := "1.5× 2× 1× 1.5×"; strings.Join(got, " ") != want {
		t.Fatalf("rates %q, want %q", got, want)
	}
	if New(b).voiceRate != 1.5 {
		t.Fatal("the speed wasn't kept")
	}
}
