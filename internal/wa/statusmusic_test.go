package wa

import (
	"testing"

	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestStatusMusic checks that a status's song is read from its picture's
// annotations or a text status's musicMetadata, and kept.
func TestStatusMusic(t *testing.T) {
	song := &waE2E.EmbeddedMusic{Author: proto.String("Hindia"), Title: proto.String("Secukupnya"), IsExplicit: proto.Bool(true)}
	photo := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{InteractiveAnnotations: []*waE2E.InteractiveAnnotation{
		{}, {EmbeddedContent: &waE2E.EmbeddedContent{Content: &waE2E.EmbeddedContent_EmbeddedMusic{EmbeddedMusic: song}}},
	}}}
	text := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("hi"), MusicMetadata: song}}
	for _, m := range []*waE2E.Message{photo, text} {
		var u model.StatusUpdate
		readMusic(&u, musicColumn(statusMusic(m)))
		if u.Song == nil || u.Song.Artist != "Hindia" || u.Song.Title != "Secukupnya" || !u.Song.Explicit {
			t.Errorf("%v: song %+v", m, u.Song)
		}
	}
	if c := musicColumn(statusMusic(&waE2E.Message{ImageMessage: &waE2E.ImageMessage{}})); c != "" {
		t.Errorf("a photo without a song has %q", c)
	}
}

// TestStatusCopyDropped checks that a status's second file, which points back
// to it, isn't kept as another update.
func TestStatusCopyDropped(t *testing.T) {
	b := &Backend{}
	video := &waE2E.VideoMessage{Mimetype: proto.String("video/mp4"), URL: proto.String("https://mmg.whatsapp.net/x"), MediaKey: []byte{1}}
	evt := func(id string, ctx *waE2E.MessageContextInfo) *events.Message {
		e := &events.Message{Message: &waE2E.Message{VideoMessage: video, MessageContextInfo: ctx}}
		e.Info.ID = id
		e.Info.Chat = types.StatusBroadcastJID
		return e
	}
	if statusCopy(evt("A", nil)) {
		t.Error("a status without an association is a copy")
	}
	copyCtx := &waE2E.MessageContextInfo{MessageAssociation: &waE2E.MessageAssociation{
		AssociationType:  waE2E.MessageAssociation_HD_VIDEO_DUAL_UPLOAD.Enum(),
		ParentMessageKey: &waCommon.MessageKey{ID: proto.String("A")},
	}}
	if _, ok := b.parseStatus(t.Context(), evt("B", copyCtx)); ok {
		t.Error("the copy of status A was kept")
	}
}
