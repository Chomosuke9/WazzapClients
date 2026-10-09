package ui

import (
	"image"
	"testing"
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
