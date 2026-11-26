package wa

import (
	"testing"

	"github.com/polymorfa/hypermeow/proto/waE2E"
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
