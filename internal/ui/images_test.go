package ui

import (
	"image"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/mock"
)

// The UI reads the entry get returned for the rest of its frame, while the
// image loads: the load mustn't write to it (go test -race), and the next
// get returns the loaded image.
func TestImageLoadKeepsEntry(t *testing.T) {
	c := newImageCache(10, 1<<20)
	loaded := make(chan struct{})
	c.invalidate = func() { close(loaded) }
	data := encodePNG(image.NewRGBA(image.Rect(0, 0, 4, 4)))
	e := c.get("k", 16, func() []byte { return data })
	states := 0
	for range 1000 {
		if e.state == imgLoading && e.size == (image.Point{}) {
			states++
		}
	}
	<-loaded
	if states != 1000 || e.state != imgLoading {
		t.Errorf("the entry changed while the UI held it: %+v", e)
	}
	got := c.get("k", 16, nil)
	if got.state != imgReady || got.size != image.Pt(4, 4) || c.bytes != 4*4*4 {
		t.Errorf("after loading: %+v, %d bytes held", got, c.bytes)
	}

	// An entry forgotten while it loads isn't put back.
	loaded = make(chan struct{})
	c.get("gone", 16, func() []byte { return data })
	c.forget("gone")
	<-loaded
	c.mu.Lock()
	_, kept := c.m["gone"]
	held := c.bytes
	c.mu.Unlock()
	if kept || held != 4*4*4 {
		t.Errorf("forgotten entry kept %v, %d bytes held", kept, held)
	}
}

// A picture decoded for a small place is decoded again for a bigger one,
// showing as it is meanwhile, and not again for a size it can't reach.
func TestImageGrows(t *testing.T) {
	c := newImageCache(10, 1<<20)
	loaded := make(chan struct{}, 10)
	c.invalidate = func() { loaded <- struct{}{} }
	data := encodePNG(image.NewRGBA(image.Rect(0, 0, 60, 80)))
	load := func() []byte { return data }
	c.get("k", 20, load)
	<-loaded
	if e := c.get("k", 20, load); e.size != image.Pt(15, 20) || !e.shrunk {
		t.Fatalf("decoded %v, shrunk %v", e.size, e.shrunk)
	}
	if e := c.get("k", 40, load); e.size != image.Pt(15, 20) {
		t.Fatalf("while it decodes again: %v", e.size)
	}
	c.get("k", 40, load) // asks once
	<-loaded
	if e := c.get("k", 40, load); e.size != image.Pt(30, 40) || c.bytes != 4*30*40 {
		t.Fatalf("decoded again %v, %d bytes held", e.size, c.bytes)
	}
	// Wide: as wide as its place, however tall.
	c.getWide("k", 40, load)
	<-loaded
	if e := c.getWide("k", 40, load); e.size.X != 40 || e.size.Y < 53 {
		t.Fatalf("wide: %v", e.size)
	}
	c.get("k", 500, load)
	<-loaded
	if e := c.get("k", 2000, load); e.size != image.Pt(60, 80) || e.shrunk {
		t.Fatalf("full size: %v, shrunk %v", e.size, e.shrunk)
	}
	select {
	case <-loaded:
		t.Error("decoded again past its size")
	default:
	}
	if len(c.growing) != 0 {
		t.Errorf("still growing: %v", c.growing)
	}
}

// Settings > Performance > Full-resolution previews is off by default.
func TestSharpPhotosPref(t *testing.T) {
	b := mock.New()
	if readPerf(b).sharpPhotos {
		t.Error("on by default")
	}
	b.SetPref(prefSharpPhotos, "on")
	if !readPerf(b).sharpPhotos {
		t.Error("not on")
	}
}
