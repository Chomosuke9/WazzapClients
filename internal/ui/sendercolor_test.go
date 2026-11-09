package ui

import (
	"fmt"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/mock"
)

// TestMemberColors checks that a chat gives its members colors like
// WhatsApp Web's WAWebChatParticipantColor: the first free one, then the
// least taken, each chat its own, kept in the prefs.
func TestMemberColors(t *testing.T) {
	b := &mock.Backend{}
	var mc memberColors
	for i := 1; i <= memberPalette; i++ {
		if c := mc.get(b, "g1@g.us", fmt.Sprintf("%d@lid", i)); c != i {
			t.Fatalf("member %d of g1 got color %d, want %d", i, c, i)
		}
	}
	if c := mc.get(b, "g1@g.us", "1@lid"); c != 1 {
		t.Errorf("a member's color changed to %d", c)
	}
	if c := mc.get(b, "g1@g.us", memberKey("1:5@lid", "")); c != 1 {
		t.Errorf("another device of a member got color %d, want 1", c)
	}
	// With all taken, the next goes to the lowest of the fewest.
	if c := mc.get(b, "g1@g.us", "18@lid"); c != 1 {
		t.Errorf("the 18th member got color %d, want 1", c)
	}
	if c := mc.get(b, "g1@g.us", "19@lid"); c != 2 {
		t.Errorf("the 19th member got color %d, want 2", c)
	}
	// Another group starts over: a member's color is per chat.
	if c := mc.get(b, "g2@g.us", "5@lid"); c != 1 {
		t.Errorf("member 5 got color %d in g2, want 1", c)
	}
	mc.endFrame(b)
	if b.Pref(prefMemberColors+"g1@g.us") == "" {
		t.Fatal("g1's colors weren't saved")
	}
	var again memberColors
	if c := again.get(b, "g1@g.us", "7@lid"); c != 7 {
		t.Errorf("after loading, member 7 got color %d, want 7", c)
	}
	for _, p := range []Palette{darkPalette, lightPalette} {
		if len(p.Members) != memberPalette {
			t.Errorf("%d member colors, want %d", len(p.Members), memberPalette)
		}
	}
}
