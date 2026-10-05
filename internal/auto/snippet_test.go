package auto

import (
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

func TestSnippetHonorsGhostAndEndsAFK(t *testing.T) {
	at := newAutoTest(t)
	s, err := at.a.SaveSnippet(model.Snippet{Body: "Hello {name}"})
	if err != nil {
		t.Fatal(err)
	}
	at.a.SetAway("lunch")
	at.a.SetPref(model.PrefGhost, "on")
	if m, err := at.a.SendSnippet("rina", s.ID, nil, model.SnippetValues("Rina Putri", "Rina Putri", "Me", "", time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC))); err == nil || m != nil {
		t.Fatal("snippet bypassed ghost")
	}
	if at.a.Away() == nil {
		t.Fatal("refused send ended AFK")
	}
	at.a.SetPref(model.PrefGhost, "off")
	m, err := at.a.SendSnippet("rina", s.ID, nil, model.SnippetValues("Rina Putri", "Rina Putri", "Me", "", time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)))
	if err != nil || m == nil || m.Text != "Hello Rina Putri" {
		t.Fatal(m, err)
	}
	if at.a.Away() != nil {
		t.Fatal("snippet send didn't end AFK")
	}
}
