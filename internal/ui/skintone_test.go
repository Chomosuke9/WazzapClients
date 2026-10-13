package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestWithTone checks that every emoji that takes a skin tone gets each
// one as a sequence the emoji font draws as one picture, which also
// means the picker can draw it through u.emojiImgs.
func TestWithTone(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"👍", "👍🏽"},
		{"✌️", "✌🏽"},
		{"🧑‍🤝‍🧑", "🧑🏽‍🤝‍🧑🏽"},
		{"⛹️‍♀️", "⛹🏽‍♀️"},
		{"😀", "😀"},
	} {
		if got := withTone(tc.in, 3); got != tc.want {
			t.Errorf("withTone(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if withTone("👍", 0) != "👍" {
		t.Error("tone 0 changes the emoji")
	}
	n := 0
	for ch := range emojiTakesTone {
		for tone := 1; tone <= len(toneMods); tone++ {
			if emojiPNG(withTone(ch, tone)) == nil {
				t.Errorf("%q in tone %d isn't one picture: %q", ch, tone, withTone(ch, tone))
			}
		}
		n++
	}
	if n < 300 {
		t.Errorf("only %d emojis take a tone", n)
	}
}

// TestPickerTone checks that the picker lists emojis in its tone and
// remembers it as an app preference.
func TestPickerTone(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.openPicker(pickComposer, nil)
	u.setTone(2)
	if got := u.backend.Pref(prefEmojiTone); got != "2" {
		t.Fatalf("pref %q", got)
	}
	found := false
	for _, r := range u.pickerRows(9) {
		for i, ch := range r.items {
			if r.bases[i] == "👍" {
				found = ch == "👍🏼"
			}
			if strings.HasPrefix(ch, "😀") && ch != "😀" {
				t.Errorf("a face got a tone: %q", ch)
			}
		}
	}
	if !found {
		t.Error("👍 isn't listed in the picked tone")
	}
	u2 := New(u.backend)
	u2.Start(func() {})
	u2.openPicker(pickComposer, nil)
	if u2.picker.tone != 2 {
		t.Errorf("tone %d after a restart", u2.picker.tone)
	}
}

// TestNewEvent checks the Create event dialog's checks and what it sends.
func TestNewEvent(t *testing.T) {
	now := testNow()
	var ev newEventState
	start := now.Add(2 * time.Hour).Truncate(time.Minute)
	ev.date.SetText(start.Format("2006-01-02"))
	ev.time.SetText(start.Format("15:04"))
	if _, _, _, ok := ev.event(now); ok {
		t.Error("an event without a name can be sent")
	}
	ev.name.SetText("Picnic")
	ev.place.SetText("The park")
	ev.guests = true
	e, _, _, ok := ev.event(now)
	if !ok || !e.Start.Equal(start) || e.Place != "The park" || !e.AllowGuests || !e.End.IsZero() {
		t.Fatalf("event %+v %v", e, ok)
	}
	ev.ends = true
	ev.endDate.SetText(start.Format("2006-01-02"))
	ev.endTime.SetText(start.Add(-time.Hour).Format("15:04"))
	if _, _, endOK, ok := ev.event(now); ok || endOK {
		t.Error("an event can end before it starts")
	}
	ev.date.SetText(now.Add(-48 * time.Hour).Format("2006-01-02"))
	ev.ends = false
	if _, startOK, _, _ := ev.event(now); startOK {
		t.Error("an event can start in the past")
	}

	u := New(mock.New())
	u.Start(func() {})
	m := u.backend.SendEvent("family", e)
	if m == nil || m.Media != model.MediaEventInvite || m.Event == nil || m.Event.Name != "Picnic" || m.Event.Place == nil {
		t.Fatalf("sent %+v", m)
	}
}
