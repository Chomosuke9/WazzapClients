package wa

import (
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestGhostPref checks that ghost mode follows its pref.
func TestGhostPref(t *testing.T) {
	b := testBackend(t)
	if b.ghost() {
		t.Fatal("ghost mode on by default")
	}
	b.SetPref(model.PrefGhost, "on")
	if !b.ghost() {
		t.Fatal("ghost mode didn't turn on")
	}
	b.SetPref(model.PrefGhost, "off")
	if b.ghost() {
		t.Fatal("ghost mode didn't turn off")
	}
}
