package offheap

import (
	"image"
	"image/color"
	"runtime"
	"testing"
	"time"
)

func TestNewRGBAFreesUnreachable(t *testing.T) {
	before := InUse()
	m := NewRGBA(image.Rect(0, 0, 300, 200))
	if got := InUse() - before; got != 4*300*200 {
		t.Fatalf("holds %d bytes, want %d", got, 4*300*200)
	}
	if m.At(299, 199) != (color.RGBA{}) {
		t.Fatal("a new picture isn't transparent")
	}
	m.Set(299, 199, color.RGBA{1, 2, 3, 4})
	if m.RGBAAt(299, 199) != (color.RGBA{1, 2, 3, 4}) {
		t.Fatal("didn't keep a pixel")
	}
	runtime.KeepAlive(m)
	m = nil
	for deadline := time.Now().Add(5 * time.Second); InUse() != before; {
		if time.Now().After(deadline) {
			t.Fatalf("still holds %d bytes after the picture went", InUse()-before)
		}
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
}

func TestNewRGBAEmpty(t *testing.T) {
	if m := NewRGBA(image.Rectangle{}); m == nil || len(m.Pix) != 0 {
		t.Fatalf("empty picture: %v", m)
	}
}
