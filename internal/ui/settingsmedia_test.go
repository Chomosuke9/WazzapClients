package ui

import (
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Search finds categories by name and settings on their pages, and a
// setting found opens its page.
func TestSettingsSearch(t *testing.T) {
	st := newSlashTest(t, "rina")
	u := st.u
	u.ShowPage("settings")
	u.settings.search.SetText("wallpaper")
	st.frame()
	var found *settingsHit
	for i, h := range u.settings.hits {
		if h.title == "Wallpaper doodles" && found == nil {
			found = &u.settings.hits[i] // Performance has it too, after
		}
	}
	if found == nil || found.category != settingChats {
		t.Fatalf("hits for wallpaper: %+v", u.settings.hits)
	}
	if u.settings.hits[0].key != "" || u.settings.hits[0].category != settingChats {
		t.Fatalf("Chats (by its description) should come first: %+v", u.settings.hits)
	}
	u.settings.search.SetText("microphone")
	st.frame()
	if len(u.settings.hits) == 0 || u.settings.hits[0].category != settingMedia {
		t.Fatalf("hits for microphone: %+v", u.settings.hits)
	}
	for q, want := range map[string]int{"performance": settingPerformance, "accounts": settingAccount} {
		u.settings.search.SetText(q)
		st.frame()
		if len(u.settings.hits) == 0 || u.settings.hits[0].category != want {
			t.Fatalf("hits for %s: %+v", q, u.settings.hits)
		}
	}
	u.settings.search.SetText("no such setting")
	st.frame()
	if len(u.settings.hits) != 0 {
		t.Fatalf("hits for nonsense: %+v", u.settings.hits)
	}
}

// With automatic photo downloads off, a picture someone sent shows its
// thumbnail until it's downloaded; your own always show.
func TestAutoPhotosOff(t *testing.T) {
	st := newSlashTest(t, "rina")
	u := st.u
	in := &model.Message{ID: "x1", ChatID: "rina", Media: model.MediaImage}
	out := &model.Message{ID: "x2", ChatID: "rina", Media: model.MediaImage, FromMe: true}
	if !u.photoWanted(in) {
		t.Fatal("photos download by default")
	}
	u.openSettings(settingStorage)
	settingRowByKey(t, u, model.PrefAutoPhotos).run()
	if u.autoPhotos || st.b.Pref(model.PrefAutoPhotos) != "off" {
		t.Fatal("the Photos switch didn't turn them off")
	}
	if u.photoWanted(in) || !u.photoWanted(out) {
		t.Fatal("with photos off, only your own pictures download")
	}
	if model.AutoDownload(st.b.Pref, model.MediaDocument) || !model.AutoDownload(st.b.Pref, model.MediaVoice) {
		t.Fatal("documents don't download by default; voice messages always do")
	}
}
