package ui

import (
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/mock"
)

func TestVolumeRemembered(t *testing.T) {
	b := mock.New()
	u := New(b)
	if u.volume != 1 {
		t.Fatalf("starts at volume %v, want 1", u.volume)
	}
	u.setVolume(0.3, false) // dragging
	if b.Pref(prefVolume) != "" {
		t.Errorf("a drag stored %q before it ended", b.Pref(prefVolume))
	}
	u.setVolume(0.3, true)
	if got := New(b).volume; got != 0.3 {
		t.Errorf("a new UI starts at volume %v, want 0.3", got)
	}

	// At volume 0, the mute button turns the sound back up.
	vv := &videoView{}
	u.setVolume(0, true)
	if u.soundOn(vv.muted) {
		t.Error("volume 0 counts as heard")
	}
	u.toggleMute(vv)
	if vv.muted || u.volume == 0 {
		t.Errorf("unmuting at volume 0 left muted %v, volume %v", vv.muted, u.volume)
	}
	u.toggleMute(vv)
	if !vv.muted {
		t.Error("the mute button didn't mute")
	}
}
