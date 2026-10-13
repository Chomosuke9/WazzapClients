package ui

import (
	"regexp"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/klipy"
	"github.com/chomosuke9/wazzapclients/internal/mock"
)

// Results fill rows edge to edge in their order, as wide as their shapes;
// the last, short row keeps its height.
func TestGIFRows(t *testing.T) {
	shape := func(w, h int) klipy.GIF { return klipy.GIF{Preview: klipy.Media{W: w, H: h}} }
	gifs := []klipy.GIF{shape(200, 100), shape(100, 100), shape(160, 90), shape(90, 160), shape(100, 100), shape(300, 100)}
	const width, rowH, gap = 600, 100, 4
	rows, heights := gifRows(gifs, width, rowH, gap)
	if len(rows) != len(heights) || len(rows) < 2 {
		t.Fatalf("rows %v heights %v", rows, heights)
	}
	next := 0
	for r, row := range rows {
		x := 0
		for _, c := range row {
			if c.i != next || c.x != x || c.w <= 0 {
				t.Fatalf("row %d: cell %+v, want result %d at %d", r, c, next, x)
			}
			next++
			x += c.w + gap
		}
		if end := x - gap; r < len(rows)-1 && end != width {
			t.Errorf("row %d ends at %d, not %d", r, end, width)
		}
	}
	if next != len(gifs) {
		t.Errorf("laid out %d of %d", next, len(gifs))
	}
	if heights[len(heights)-1] != rowH {
		t.Errorf("last row is %d tall", heights[len(heights)-1])
	}
	if rows, _ := gifRows(nil, width, rowH, gap); len(rows) != 0 {
		t.Errorf("no results: %v", rows)
	}
}

// Favourites are kept as slugs, newest first, and found whichever way a
// slug is written (search adds "--…"; Recent and Items don't). Taking one
// off on the Favourites tab takes it off the tab at once.
func TestGIFFavs(t *testing.T) {
	u := New(mock.New())
	g := &u.gifs
	a, b := klipy.GIF{Slug: "a-1--x"}, klipy.GIF{Slug: "b-2"}
	u.toggleFav(a)
	u.toggleFav(b)
	if got := u.backend.Pref(prefKlipyFavs); got != "b-2 a-1--x" {
		t.Fatalf("saved %q", got)
	}
	if !g.isFav("a-1") || !g.isFav("b-2--y") || g.isFav("c-3") {
		t.Error("isFav")
	}
	g.src, g.results = gifSource{set: gifFavs}, []klipy.GIF{{Slug: "b-2"}, {Slug: "a-1"}}
	u.toggleFav(klipy.GIF{Slug: "a-1"})
	if g.isFav("a-1") || len(g.results) != 1 || g.results[0].Slug != "b-2" {
		t.Errorf("favs %v, results %v", g.favs, g.results)
	}
	u.loadGIFPrefs()
	if len(g.favs) != 1 || g.favs[0] != "b-2" {
		t.Errorf("read back %v", g.favs)
	}
}

func TestNewUUID(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if id := newUUID(); !re.MatchString(id) || id == newUUID() {
		t.Errorf("uuid %q", id)
	}
}

// GIF search is on until it's turned off.
func TestGIFSearchDefault(t *testing.T) {
	u := New(mock.New())
	if !u.gifs.on {
		t.Fatal("off by default")
	}
	setExtra(u.backend, prefKlipy, false)
	u.loadGIFPrefs()
	if u.gifs.on {
		t.Error("still on once turned off")
	}
}
