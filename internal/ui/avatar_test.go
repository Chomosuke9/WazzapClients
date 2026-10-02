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
		"⁨~Vivy⁩":      "V",
	} {
		if got := avatarInitial(name); got != want {
			t.Errorf("avatarInitial(%q) = %q, want %q", name, got, want)
		}
	}
	if avatarTint("a@lid", "", 7) != avatarTint("a@lid", "Other name", 7) {
		t.Error("the color depends on the name, not just the ID")
	}
}
