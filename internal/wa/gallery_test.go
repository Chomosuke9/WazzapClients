package wa

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestGallery checks the Media panel's pages: each kind picks its
// messages from every chat but channels (or from one), newest or oldest
// first, a page at a time, and the search matches text and file names.
func TestGallery(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &msgStore{db: db}
	if err := s.init(ctx); err != nil {
		t.Fatal(err)
	}
	img, vid, doc := int(model.MediaImage), int(model.MediaVideo), int(model.MediaDocument)
	for _, m := range []struct {
		chat, id string
		ts       int
		kind     model.Kind
		media    int
		text     string
		starred  int
		file     string
	}{
		{"a@g.us", "1", 1, model.KindImage, img, "Beach", 0, ""},
		{"b@s.whatsapp.net", "2", 2, model.KindImage, vid, "", 1, ""},
		{"a@g.us", "3", 3, model.KindDeleted, img, "", 0, ""},
		{"x@newsletter", "4", 4, model.KindImage, img, "", 0, ""},
		{"a@g.us", "5", 5, model.KindText, doc, "", 0, `{"n":"Résumé.pdf"}`},
		{"b@s.whatsapp.net", "6", 6, model.KindText, 0, "see https://example.com", 1, ""},
		{"a@g.us", "7", 7, model.KindImage, img, "", 0, ""},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO wz_messages (chat, id, ts, kind, media, text, starred, file)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, m.chat, m.id, m.ts, int(m.kind), m.media, m.text, m.starred, m.file); err != nil {
			t.Fatal(err)
		}
	}
	page := func(q model.GalleryQuery) string {
		if q.Limit == 0 {
			q.Limit = 10
		}
		raw, more, err := s.gallery(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, r := range raw {
			ids = append(ids, r.ID)
		}
		out := strings.Join(ids, " ")
		if more {
			out += " +"
		}
		return out
	}
	for _, c := range []struct {
		q    model.GalleryQuery
		want string
	}{
		{model.GalleryQuery{Kind: model.GalleryMedia}, "7 2 1"},
		{model.GalleryQuery{Kind: model.GalleryMedia, Oldest: true}, "1 2 7"},
		{model.GalleryQuery{Kind: model.GalleryMedia, Limit: 2}, "7 2 +"},
		{model.GalleryQuery{Kind: model.GalleryMedia, Limit: 2, Offset: 2}, "1"},
		{model.GalleryQuery{Kind: model.GalleryMedia, ChatID: "a@g.us"}, "7 1"},
		{model.GalleryQuery{Kind: model.GalleryMedia, Text: "beach"}, "1"},
		{model.GalleryQuery{Kind: model.GalleryDocs}, "5"},
		{model.GalleryQuery{Kind: model.GalleryDocs, Text: "RÉSUMÉ"}, "5"},
		{model.GalleryQuery{Kind: model.GalleryLinks}, "6"},
		{model.GalleryQuery{Kind: model.GalleryStarred}, "6 2"},
		{model.GalleryQuery{Kind: model.GalleryStarred, Text: "example", Limit: 1}, "6"},
	} {
		if got := page(c.q); got != c.want {
			t.Errorf("gallery %+v = %q, want %q", c.q, got, c.want)
		}
	}
}
