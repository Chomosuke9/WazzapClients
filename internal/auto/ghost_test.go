package auto

import (
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestGhost checks that ghost mode refuses what you send, with a notice,
// but not scheduled messages or AFK replies, and that it lasts.
func TestGhost(t *testing.T) {
	at := newAutoTest(t)
	at.a.Schedule(Job{Chat: "work", At: base.Add(time.Minute), Text: "standup"})
	at.a.SetAway("lunch")
	at.a.SetPref(model.PrefGhost, "on")
	if !at.a.Ghost() || !Wrap(at.f, time.Now).Ghost() {
		t.Fatal("ghost mode isn't on, or doesn't last")
	}
	before := len(at.f.Messages("rina", 1000))
	last := at.f.Messages("rina", 1)[0]
	if m := at.a.Send("rina", model.Draft{Text: "hi"}); m != nil {
		t.Fatal("sent in ghost mode")
	}
	at.a.React(last, "👍")
	at.a.Delete(last, true)
	at.a.Forward([]*model.Message{last}, []string{"work"})
	if at.a.NewAlbum("rina", 2, 0) != "" {
		t.Error("opened an album in ghost mode")
	}
	if _, notices := at.poll(); len(notices) != 4 || notices[0] != GhostText {
		t.Errorf("notices %q", notices)
	}
	if now := at.f.Messages("rina", 1000); len(now) != before || len(now[len(now)-1].Reactions) != 0 ||
		now[len(now)-1].Kind == model.KindDeleted {
		t.Error("something went through in ghost mode")
	}
	if at.a.Away() == nil {
		t.Fatal("a refused message ended AFK")
	}
	// Deleting for you isn't seen by anyone.
	at.a.Delete(last, false)
	if _, notices := at.poll(); len(notices) != 0 {
		t.Errorf("deleting for you was refused: %q", notices)
	}
	// What it sends for you still goes.
	at.f.in = append(at.f.in, incoming("x1", "rina", "rina@lid", "hey", at.now))
	at.now = base.Add(2 * time.Minute)
	sent, _ := at.poll()
	if len(sent) != 2 || sent[0].Text != AwayText(at.a.Away(), at.now) || sent[1].Text != "standup" {
		t.Fatalf("sent %d messages for you in ghost mode", len(sent))
	}
	at.a.SetPref(model.PrefGhost, "off")
	if m := at.a.Send("rina", model.Draft{Text: "back"}); m == nil {
		t.Fatal("ghost mode off still refuses")
	}
}
