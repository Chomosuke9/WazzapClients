package ui

import "testing"

// TestAvatarInitial checks which names show a letter instead of the
// placeholder silhouette.
func TestAvatarInitial(t *testing.T) {
	for name, want := range map[string]string{
		"Caca Mocha":   "C",
		"~raza":        "R",
		"élodie":       "É",
		"+62 812-3456": "",
		"😀 Fun":        "",
		"":             "",
		"⁨~Nova⁩":      "N",
	} {
		if got := avatarInitial(name); got != want {
			t.Errorf("avatarInitial(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestPersonTint checks that a color comes from the last digit of the ID's
// user, like WhatsApp's, with one color of each kind for every digit.
func TestPersonTint(t *testing.T) {
	for id, want := range map[string]int{
		"10000000000007@lid":             7,
		"10000000000005:12@lid":          5,
		"6281200000009@s.whatsapp.net":   9,
		"6281200000003:4@s.whatsapp.net": 3,
		"120363000000000008@g.us":        8,
	} {
		if got := personTint(id, "A name"); got != want {
			t.Errorf("personTint(%q) = %d, want %d", id, got, want)
		}
	}
	if personTint("nova@lid", "") != personTint("nova@lid", "Other name") {
		t.Error("the color depends on the name, not just the ID")
	}
	if a := personTint("", "Ana"); a < 0 || a > 9 {
		t.Errorf("personTint without an ID = %d, want 0 to 9", a)
	}
	for _, p := range []Palette{darkPalette, lightPalette} {
		if len(p.Senders) != 10 || len(p.AvatarBgs) != 10 {
			t.Errorf("%d sender colors and %d avatar backgrounds, want 10 of each", len(p.Senders), len(p.AvatarBgs))
		}
	}
}
