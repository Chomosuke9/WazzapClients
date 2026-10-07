package auto

import (
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

func TestWhisperGhostAndAFK(t *testing.T) {
	at := newAutoTest(t)
	at.a.SetPref("cmd_whisper", "on")
	at.a.SetAway("lunch")
	at.a.SetPref(model.PrefGhost, "on")
	if err := <-at.a.SendWhisper("work", []string{"budi@lid"}, "secret"); err == nil || err.Error() != GhostText {
		t.Fatalf("ghost mode: %v", err)
	}
	if at.a.Away() == nil {
		t.Fatal("refused whisper ended AFK")
	}
	at.a.SetPref(model.PrefGhost, "off")
	before := len(at.f.Messages("work", 1000))
	if err := <-at.a.SendWhisper("work", []string{"budi@lid"}, "secret"); err != nil {
		t.Fatal(err)
	}
	if at.a.Away() != nil {
		t.Fatal("whisper didn't end AFK")
	}
	if len(at.f.Messages("work", 1000)) != before {
		t.Fatal("whisper became ordinary group message")
	}
}
