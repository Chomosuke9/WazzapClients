package wa

import (
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// A video note as it comes from WhatsApp.
const ptvJSON = `{
	"messageContextInfo": {"messageSecret": "EADs3J4evhv/kuAOBxLmSP2582VmBglpSgetntwkF3M="},
	"ptvMessage": {
		"URL": "https://mmg.whatsapp.net/v/t62.7161-24/1_n.enc",
		"mimetype": "video/mp4",
		"fileSHA256": "JRzt3Jb0iwgMdwbtTXCosNK8Ozuu37nQPLQC+GQwpHc=",
		"fileLength": "256130",
		"seconds": 1,
		"mediaKey": "wiSyTzuNfIt69UPiSNGdGQir4Xs9JHgG4nUzH9VYt1M=",
		"height": 576,
		"width": 576,
		"directPath": "/v/t62.7161-24/1_n.enc",
		"JPEGThumbnail": "/9j/4AAQ",
		"videoSourceType": "USER_VIDEO"
	}
}`

func ptvMessage(t *testing.T) *waE2E.Message {
	t.Helper()
	var m waE2E.Message
	if err := protojson.Unmarshal([]byte(ptvJSON), &m); err != nil {
		t.Fatal(err)
	}
	return &m
}

func TestDescribeVideoNote(t *testing.T) {
	m := ptvMessage(t)
	c := describe(m)
	if !c.round || c.media != model.MediaVideo || c.kind != model.KindImage || c.duration != 1 || len(c.thumb) == 0 {
		t.Fatalf("described as %+v", c)
	}
	if !hasMediaKey(m) {
		t.Error("its media key wasn't seen")
	}
	if describe(&waE2E.Message{VideoMessage: &waE2E.VideoMessage{}}).round {
		t.Error("a plain video is round")
	}

	// A stored message reads back as a video note from its payload alone.
	r := rawMsg{Message: &model.Message{Kind: model.KindImage, Media: model.MediaVideo}}
	r.fill(marshal(m), nil)
	if !r.Round || r.Duration != 1 || !r.hasBlob {
		t.Fatalf("filled as %+v, blob %v", r.Message, r.hasBlob)
	}
}

// What's kept of a video note downloads like a video, and goes on as a video
// note when it's forwarded or quoted.
func TestVideoNoteBlobGoesBack(t *testing.T) {
	m := ptvMessage(t)
	c := describe(m)
	blob := c.keptBlob(m)
	if downloadable(c.media, blob) == nil {
		t.Fatal("it doesn't download")
	}
	again := mediaMessage(c.media, blob)
	asVideoNote(again)
	ci := &waE2E.ContextInfo{IsForwarded: proto.Bool(true)}
	setContext(again, ci)
	if again.VideoMessage != nil || again.GetPtvMessage().GetDirectPath() != m.GetPtvMessage().GetDirectPath() {
		t.Fatalf("sends as %v", again)
	}
	if !again.GetPtvMessage().GetContextInfo().GetIsForwarded() {
		t.Error("its context was lost")
	}
	if !describe(again).round {
		t.Error("it isn't round any more")
	}
}

func TestVideoNoteNotEditable(t *testing.T) {
	now := time.Date(2026, 11, 6, 15, 0, 0, 0, time.UTC)
	m := &model.Message{FromMe: true, Kind: model.KindImage, Media: model.MediaVideo, Receipt: model.Sent, Time: now}
	if !m.CanEdit(now) {
		t.Fatal("a video's caption should be editable")
	}
	m.Round = true
	if m.CanEdit(now) {
		t.Error("a video note has no caption to edit")
	}
}
