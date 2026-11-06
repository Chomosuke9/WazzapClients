package ui

import (
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

func TestVideoNoteLabel(t *testing.T) {
	m := &model.Message{Kind: model.KindImage, Media: model.MediaVideo, Round: true}
	if got := mediaLabel(m); got != "Video note" {
		t.Errorf("label %q", got)
	}
	if !isRound(m) {
		t.Error("not drawn round")
	}
	m.Kind = model.KindViewOnce
	if isRound(m) {
		t.Error("a view once video note should stay a view once card")
	}
}

// Only one plays with sound, voice messages stop for it, and it lets go of
// the sound when it stops being drawn.
func TestRoundSoundFollowsClicks(t *testing.T) {
	u := &UI{}
	a := &model.Message{ChatID: "c", ID: "a", Kind: model.KindImage, Media: model.MediaVideo, Round: true}
	b := &model.Message{ChatID: "c", ID: "b", Kind: model.KindImage, Media: model.MediaVideo, Round: true}
	u.toggleRound(a)
	if u.players.sound != "c/a" {
		t.Fatalf("sound on %q", u.players.sound)
	}
	u.toggleRound(b)
	if u.players.sound != "c/b" {
		t.Fatalf("sound on %q", u.players.sound)
	}
	u.players.endFrame() // drawn this frame: it stays
	if u.players.sound != "c/b" {
		t.Fatal("lost the sound while on screen")
	}
	u.players.endFrame() // not drawn
	if u.players.sound != "" {
		t.Fatalf("sound stayed on %q", u.players.sound)
	}
}
