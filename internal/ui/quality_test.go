package ui

import (
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// The photo quality picked last is the next photos', after a restart too.
func TestQualityRemembered(t *testing.T) {
	b := mock.New()
	u := New(b)
	if u.attach.quality != model.QualityStandard {
		t.Fatalf("starts at quality %v", u.attach.quality)
	}
	u.attach.files = []*attachFile{{Attachment: model.Attachment{Path: "a.jpg", Media: model.MediaImage}}}
	for _, it := range u.qualityMenuItems() {
		if it.key == "hd" {
			it.run()
		}
	}
	if u.attach.quality != model.QualityHD || b.Pref(prefPhotoQuality) != "hd" {
		t.Fatalf("after picking HD: %v, pref %q", u.attach.quality, b.Pref(prefPhotoQuality))
	}
	if q := New(b).attach.quality; q != model.QualityHD {
		t.Fatalf("a new window sends at %v", q)
	}
	// Raw comes back only while its extra feature is on.
	b.SetPref(prefPhotoQuality, "raw")
	if q := New(b).attach.quality; q != model.QualityHD {
		t.Fatalf("Raw without its feature: %v", q)
	}
	b.SetPref(prefRawPhotos, "on")
	if q := New(b).attach.quality; q != model.QualityRaw {
		t.Fatalf("Raw with its feature: %v", q)
	}
}
