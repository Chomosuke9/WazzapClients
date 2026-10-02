package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestAlbumRows checks an album's pictures share one list row, which a
// caption, a reply, another album or another sender takes a picture out of,
// and that a lone picture of an album is an ordinary row.
func TestAlbumRows(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("rina")
	at := testNow()
	pic := func(id, album string) *model.Message {
		return &model.Message{ID: id, ChatID: "rina", Kind: model.KindImage, Media: model.MediaImage, Time: at, Album: album}
	}
	captioned := pic("a4", "A")
	captioned.Text = "look"
	reply := pic("a6", "A")
	reply.Quote = &model.Quote{Text: "hi"}
	mine := pic("a8", "A")
	mine.FromMe = true
	u.msgs = []*model.Message{
		pic("a1", "A"), pic("a2", "A"), pic("a3", "A"), captioned, pic("a5", "A"), reply, pic("a7", "A"),
		mine, pic("b1", "B"), pic("b2", "B"), pic("c1", "C"), pic("x1", ""), pic("x2", ""),
	}
	u.msgsVer++
	var got [][]string
	for _, r := range u.rows(u.selected) {
		if r.kind != rowMessage {
			continue
		}
		ids := []string{r.msg.ID}
		if r.group != nil {
			if !r.album {
				t.Errorf("row of %s groups pictures as stickers", r.msg.ID)
			}
			ids = nil
			for _, m := range r.group {
				ids = append(ids, m.ID)
			}
		}
		got = append(got, ids)
	}
	want := [][]string{{"a1", "a2", "a3"}, {"a4"}, {"a5"}, {"a6", "a7"}, {"a8"}, {"b1", "b2"}, {"c1"}, {"x1"}, {"x2"}}
	if len(got) != len(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	for i := range want {
		if len(got[i]) != len(want[i]) || got[i][0] != want[i][0] {
			t.Errorf("row %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestAlbumTiles checks the grid's tiles fit its width, don't overlap, and
// that it shows at most four.
func TestAlbumTiles(t *testing.T) {
	const w, gap = 330, 3
	for n := 1; n <= 7; n++ {
		tiles := albumTiles(n, w, gap)
		if want := min(n, 4); len(tiles) != want {
			t.Errorf("%d pictures: %d tiles, want %d", n, len(tiles), want)
		}
		for i, a := range tiles {
			if a.Empty() || !a.In(image.Rect(0, 0, w, 2*w)) {
				t.Errorf("%d pictures: tile %d is %v", n, i, a)
			}
			for _, b := range tiles[i+1:] {
				if a.Overlaps(b) {
					t.Errorf("%d pictures: tiles %v and %v overlap", n, a, b)
				}
			}
		}
	}
}

// TestSendAlbum checks photos sent together go in one album, and a
// document sent with them doesn't.
func TestSendAlbum(t *testing.T) {
	u := New(mock.New())
	u.Start(func() {})
	u.SelectID("rina")
	dir := t.TempDir()
	var paths []string
	for _, name := range []string{"a.png", "b.png"} {
		img := image.NewNRGBA(image.Rect(0, 0, 40, 30))
		img.SetNRGBA(1, 1, color.NRGBA{R: 0xff, A: 0xff})
		p := filepath.Join(dir, name)
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		png.Encode(f, img)
		f.Close()
		paths = append(paths, p)
	}
	doc := filepath.Join(dir, "notes.txt")
	os.WriteFile(doc, []byte("hello"), 0o600)
	u.attachPaths(append(paths, doc))
	if len(u.attach.files) != 3 {
		t.Fatalf("attachPaths added %d files, want 3", len(u.attach.files))
	}
	old := map[*model.Message]bool{}
	for _, m := range u.msgs {
		old[m] = true
	}
	u.sendComposer()
	for i := 0; i < 100 && len(u.attach.outbox) > 0; i++ {
		u.flushOutbox()
		time.Sleep(5 * time.Millisecond)
	}
	var sent []*model.Message
	for _, m := range u.msgs {
		if !old[m] {
			sent = append(sent, m)
		}
	}
	if len(sent) != 3 {
		t.Fatalf("sent %d messages, want 3", len(sent))
	}
	if sent[0].Album == "" || sent[1].Album != sent[0].Album {
		t.Errorf("the photos went in albums %q and %q", sent[0].Album, sent[1].Album)
	}
	if sent[2].Album != "" {
		t.Errorf("the document went in album %q", sent[2].Album)
	}
}
