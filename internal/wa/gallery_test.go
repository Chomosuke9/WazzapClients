package wa

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

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
	resume := marshal(&waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{FileName: proto.String("Résumé.pdf"),
		Caption: proto.String("For the job")}})
	for _, m := range []struct {
		chat, id string
		ts       int
		kind     model.Kind
		media    int
		text     string
		starred  int
		payload  []byte
	}{
		{"a@g.us", "1", 1, model.KindImage, img, "Beach", 0, nil},
		{"b@s.whatsapp.net", "2", 2, model.KindImage, vid, "", 1, nil},
		{"a@g.us", "3", 3, model.KindDeleted, img, "", 0, nil},
		{"x@newsletter", "4", 4, model.KindImage, img, "", 0, nil},
		{"a@g.us", "5", 5, model.KindText, doc, "For the job", 0, resume},
		{"b@s.whatsapp.net", "6", 6, model.KindText, 0, "see https://example.com", 1, nil},
		{"a@g.us", "7", 7, model.KindImage, img, "", 0, nil},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO wz_messages (chat, id, ts, kind, media, text, starred, raw_payload)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, m.chat, m.id, m.ts, int(m.kind), m.media, m.text, m.starred, m.payload); err != nil {
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
