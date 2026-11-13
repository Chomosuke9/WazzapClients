package wa

import (
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestWipeKeepsAppPrefs: logging an account out wipes its data, but the
// app's preferences (the theme...) stay, for the next account to take
// over.
func TestWipeKeepsAppPrefs(t *testing.T) {
	b := testBackend(t)
	b.KeepPrefs([]string{"theme", "accent"})
	b.SetPref("theme", "light")
	b.SetPref("accent", "red")
	b.SetPref("afk", "on")
	b.SetPref(model.PrefHistorySync, "off")
	if err := b.store.ensureChat(b.ctx, b.db, "a@lid", false, "A"); err != nil {
		t.Fatal(err)
	}

	b.wipeData()
	for k, want := range map[string]string{"theme": "light", "accent": "red", "afk": "", model.PrefHistorySync: "off"} {
		if got := b.Pref(k); got != want {
			t.Errorf("pref %q is %q after the wipe, want %q", k, got, want)
		}
	}
	if chats := b.Chats(); len(chats) != 0 {
		t.Errorf("%d chats left after the wipe", len(chats))
	}

	// Without anything to keep, every preference but the history sync
	// choice goes.
	b.KeepPrefs(nil)
	b.wipeData()
	if got := b.Pref("theme"); got != "" {
		t.Errorf("theme is %q after a wipe that keeps nothing", got)
	}
}
