package wa

import (
	"bytes"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

// TestLinkPreview checks a received link preview is kept with its picture
// and read back with the message.
func TestLinkPreview(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	group := types.NewJID("123", types.GroupServer)
	chat := group.String()
	if err := b.store.ensureChat(ctx, b.db, chat, true, "Group"); err != nil {
		t.Fatal(err)
	}
	pic := []byte{0xff, 0xd8, 0xff, 0xd9}
	b.onMessage(&events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: group, Sender: types.NewJID("555", types.HiddenUserServer), IsGroup: true},
			ID:            "L1", Timestamp: time.Unix(1_700_000_000, 0),
		},
		Message: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text:          proto.String("look https://go.dev/blog"),
			MatchedText:   proto.String("https://go.dev/blog"),
			Title:         proto.String("The Go Blog"),
			Description:   proto.String("News from the Go team"),
			JPEGThumbnail: pic,
		}},
	})
	r, ok := b.store.message(ctx, chat, "L1")
	if !ok {
		t.Fatal("the message isn't stored")
	}
	l := r.Link
	if l == nil || l.URL != "https://go.dev/blog" || l.Title != "The Go Blog" || l.Description != "News from the Go team" {
		t.Fatalf("link = %+v", l)
	}
	if !bytes.Equal(r.Thumb, pic) {
		t.Fatalf("thumb = %v", r.Thumb)
	}
	if !l.Shown(r.Text) || l.Shown("look") {
		t.Fatal("the preview shows only while the text has its link")
	}
}

// TestLinkPreviewImage checks a received preview's big picture is kept
// where it can be downloaded from, with its size, and goes with a forward.
func TestLinkPreviewImage(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	group := types.NewJID("123", types.GroupServer)
	chat := group.String()
	if err := b.store.ensureChat(ctx, b.db, chat, true, "Group"); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{7}, 32)
	b.onMessage(&events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: group, Sender: types.NewJID("555", types.HiddenUserServer), IsGroup: true},
			ID:            "L2", Timestamp: time.Unix(1_700_000_000, 0),
		},
		Message: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text:                proto.String("https://github.com/x/y"),
			MatchedText:         proto.String("https://github.com/x/y"),
			Title:               proto.String("x/y"),
			JPEGThumbnail:       []byte{0xff, 0xd8, 0xff, 0xd9},
			ThumbnailDirectPath: proto.String("/v/t62/abc.enc"),
			MediaKey:            key,
			ThumbnailWidth:      proto.Uint32(1024),
			ThumbnailHeight:     proto.Uint32(512),
		}},
	})
	r, ok := b.store.message(ctx, chat, "L2")
	if !ok {
		t.Fatal("the message isn't stored")
	}
	if l := r.Link; l == nil || l.W != 1024 || l.H != 512 {
		t.Fatalf("link = %+v", r.Link)
	}
	_, blob, err := b.store.mediaBlob(ctx, chat, "L2")
	if err != nil || len(blob) == 0 {
		t.Fatalf("blob %v, %v", blob, err)
	}
	var e waE2E.ExtendedTextMessage
	copyLinkImage(&e, blob)
	if e.GetThumbnailDirectPath() != "/v/t62/abc.enc" || !bytes.Equal(e.GetMediaKey(), key) || e.GetThumbnailWidth() != 1024 {
		t.Fatalf("copied %v", &e)
	}
}
