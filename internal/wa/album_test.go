package wa

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestAlbumOf checks a picture is filed under its album wherever its
// messageContextInfo sits, and the message that goes out points at it.
func TestAlbumOf(t *testing.T) {
	pic := func() *waE2E.Message { return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}} }
	chat := types.NewJID("123", types.DefaultUserServer)

	direct := pic()
	inAlbum(direct, chat, "ALBUM1")
	if got := albumOf(direct); got != "ALBUM1" {
		t.Errorf("a picture sent in an album is in %q", got)
	}
	if len(direct.GetMessageContextInfo().GetMessageSecret()) != 32 {
		t.Error("a picture of an album goes without a message secret")
	}

	child := pic()
	inAlbum(child, chat, "ALBUM2")
	wrapped := &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{
		AssociatedChildMessage: &waE2E.FutureProofMessage{Message: child},
	}}}
	if got := albumOf(wrapped); got != "ALBUM2" {
		t.Errorf("a wrapped picture is in %q", got)
	}

	other := pic()
	inAlbum(other, chat, "ORIGINAL")
	other.MessageContextInfo.MessageAssociation.AssociationType = waE2E.MessageAssociation_HD_IMAGE_DUAL_UPLOAD.Enum()
	if got := albumOf(other); got != "" {
		t.Errorf("an HD copy is in album %q", got)
	}
	if got := albumOf(pic()); got != "" {
		t.Errorf("a plain picture is in album %q", got)
	}
}

// TestAlbumStored checks a picture's album survives the store, and that
// storing it again without one (as an edit or a resend does) keeps it.
func TestAlbumStored(t *testing.T) {
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
	m := &model.Message{ID: "p1", ChatID: "c@s.whatsapp.net", Kind: model.KindImage, Media: model.MediaImage,
		Time: time.Unix(1000, 0), Album: "ALBUM"}
	if err := s.putMessage(ctx, db, storedMsg{Message: m}); err != nil {
		t.Fatal(err)
	}
	cp := *m
	cp.Album = ""
	if err := s.putMessage(ctx, db, storedMsg{Message: &cp}); err != nil {
		t.Fatal(err)
	}
	r, ok := s.message(ctx, m.ChatID, m.ID)
	if !ok {
		t.Fatal("the picture isn't stored")
	}
	if r.Album != "ALBUM" {
		t.Errorf("stored album %q, want ALBUM", r.Album)
	}
}
