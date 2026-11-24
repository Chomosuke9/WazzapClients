package ui

import (
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// A sticker's speaker asks for its sound, and a second click, or a
// download that failed, leaves it silent. Another one takes its turn.
func TestStickerSound(t *testing.T) {
	t.Setenv("WAZZAP_DEMO_AUDIO", "")
	b := mock.New()
	u := New(b)
	u.Start(func() {})
	u.SelectID("rina")
	one := &model.Message{ID: "s1", ChatID: "rina", Kind: model.KindSticker, Media: model.MediaSticker, Sound: true}
	two := &model.Message{ID: "s2", ChatID: "rina", Kind: model.KindSticker, Media: model.MediaSticker, Sound: true}

	u.toggleStickerSound(one)
	if u.voice.key != fileKey(one) || !u.voice.loading {
		t.Fatalf("voice = %+v, want the first one loading", u.voice)
	}
	u.toggleStickerSound(two)
	if u.voice.key != fileKey(two) {
		t.Fatalf("voice = %+v, want the second one", u.voice)
	}
	u.toggleStickerSound(two)
	if u.voice.key != "" {
		t.Fatalf("voice = %+v, want none after a second click", u.voice)
	}
	u.toggleStickerSound(one)
	u.fileDownloaded(model.MediaEvent{ChatID: "rina", MsgID: "s1", Failed: true})
	if u.voice.key != "" {
		t.Fatalf("voice = %+v, want none after a failed download", u.voice)
	}
}
