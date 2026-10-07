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
	if err := <-at.a.SendWhisper("work", []string{"budi@lid"}, "secret", nil); err == nil || err.Error() != GhostText {
		t.Fatalf("ghost mode: %v", err)
	}
	if at.a.Away() == nil {
		t.Fatal("refused whisper ended AFK")
	}
	at.a.SetPref(model.PrefGhost, "off")
	before := len(at.f.Messages("work", 1000))
	if err := <-at.a.SendWhisper("work", []string{"bima"}, "secret", nil); err != nil {
		t.Fatal(err)
	}
	if at.a.Away() != nil {
		t.Fatal("whisper didn't end AFK")
	}
	// A whisper leaves exactly one local record, marked as a whisper rather
	// than broadcast as an ordinary group message.
	msgs := at.f.Messages("work", 1000)
	if len(msgs) != before+1 {
		t.Fatalf("whisper left %d records, want 1", len(msgs)-before)
	}
	if w := msgs[len(msgs)-1]; len(w.Whisper) == 0 {
		t.Fatal("whisper stored as an ordinary message, not a local record")
	}
}
