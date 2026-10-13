package emojiset

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/offheap"
)

func testSheet(side, frames int, c color.RGBA) Sheet {
	cols := 3
	rows := (frames + cols - 1) / cols
	sh := Sheet{Side: side, Cols: cols, Image: offheap.NewRGBA(image.Rect(0, 0, cols*side, rows*side))}
	for i := range frames {
		sh.Delays = append(sh.Delays, time.Duration(30+i)*time.Millisecond)
		at := image.Pt(i%cols*side, i/cols*side)
		sh.Image.SetRGBA(at.X+side/2, at.Y+side/2, color.RGBA{c.R, c.G, uint8(i), c.A})
	}
	return sh
}

func TestSheetRoundTrip(t *testing.T) {
	s := New(t.TempDir())
	if s.HasSheet("1f600", 30) {
		t.Fatal("has a sheet before any was written")
	}
	want := testSheet(30, 5, color.RGBA{R: 200, A: 255})
	s.WriteSheet("1f600", want)
	if !s.HasSheet("1f600", 30) || s.HasSheet("1f600", 31) || s.HasSheet("1f601", 30) {
		t.Fatal("HasSheet doesn't say which sheet was written")
	}
	// A new store (the next run) finds it on disk.
	s = New(s.Dir())
	got, ok := s.ReadSheet("1f600", 30)
	if !ok {
		t.Fatal("couldn't read the sheet written")
	}
	if got.Side != 30 || got.Cols != 3 || !slices.Equal(got.Delays, want.Delays) || got.Image.Bounds() != want.Image.Bounds() {
		t.Fatalf("read side %d, %d columns, delays %v on %v", got.Side, got.Cols, got.Delays, got.Image.Bounds())
	}
	if !slices.Equal(got.Image.Pix, want.Image.Pix) {
		t.Fatal("read other pixels than were written")
	}
	if s.Size() == 0 {
		t.Error("Size doesn't count sheets")
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if s.HasSheet("1f600", 30) {
		t.Error("Clear kept the sheet")
	}
}

func TestSheetDamaged(t *testing.T) {
	s := New(t.TempDir())
	s.WriteSheet("1f600", testSheet(30, 5, color.RGBA{R: 200, A: 255}))
	p := filepath.Join(s.Dir(), sheetsDir, sheetName("1f600", 30))
	b, _ := os.ReadFile(p)
	os.WriteFile(p, b[:len(b)/2], 0o600)
	if _, ok := s.ReadSheet("1f600", 30); ok {
		t.Fatal("read a cut sheet")
	}
	if s.HasSheet("1f600", 30) {
		t.Error("still has the cut sheet")
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("the cut sheet stays on disk")
	}
	// What a write cut short leaves goes on the next run.
	part := filepath.Join(s.Dir(), sheetsDir, "1f601-30.sheet.123.part")
	os.WriteFile(part, []byte("half"), 0o600)
	New(s.Dir()).HasSheet("1f601", 30)
	if _, err := os.Stat(part); err == nil {
		t.Error("a part file stays")
	}
}

func TestSheetEviction(t *testing.T) {
	old := sheetsMax
	t.Cleanup(func() { sheetsMax = old })
	s := New(t.TempDir())
	// Noise doesn't compress, so each sheet takes about 4*30*30*9 bytes.
	noisy := func(seed byte) Sheet {
		sh := testSheet(30, 9, color.RGBA{})
		for i := range sh.Image.Pix {
			sh.Image.Pix[i] = byte(i*7) ^ seed ^ byte(i>>8*13)
		}
		return sh
	}
	s.WriteSheet("a", noisy(1))
	one := s.sheetsSize
	sheetsMax = 3*one + one/2
	s.WriteSheet("b", noisy(2))
	s.WriteSheet("c", noisy(3))
	time.Sleep(20 * time.Millisecond)
	if _, ok := s.ReadSheet("a", 30); !ok { // a is now read last
		t.Fatal("couldn't read a")
	}
	s.WriteSheet("d", noisy(4)) // over: down to 3/4 of the most
	if !s.HasSheet("a", 30) || !s.HasSheet("d", 30) {
		t.Error("let go of a sheet read or written last")
	}
	if s.HasSheet("b", 30) {
		t.Error("kept the sheet read longest ago")
	}
	if s.sheetsSize > sheetsMax*3/4 {
		t.Errorf("%d bytes of sheets kept, the most is %d", s.sheetsSize, sheetsMax)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), sheetsDir, sheetName("b", 30))); err == nil {
		t.Error("an evicted sheet stays on disk")
	}
}
