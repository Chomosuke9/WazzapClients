package wa

import (
	"strings"

	"github.com/polymorfa/hypermeow/proto/waE2E"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// A status update with a song names it in an interactive annotation of
// its picture or video (embeddedContent.embeddedMusic), or a text status
// in its musicMetadata, as WhatsApp Web reads them
// (getFirstMusicAnnotationEmbeddedContent, WAWebMusicParsingUtils). The
// song plays from Meta's music service, which linked devices don't reach,
// so only its artist and title show (wz_status.music: author, title and
// "1" when it's explicit, a line each).

// statusMusic returns the song of a status update, or nil.
func statusMusic(m *waE2E.Message) *waE2E.EmbeddedMusic {
	var notes []*waE2E.InteractiveAnnotation
	switch {
	case m.GetImageMessage() != nil:
		notes = append(m.GetImageMessage().GetInteractiveAnnotations(), m.GetImageMessage().GetAnnotations()...)
	case m.GetVideoMessage() != nil:
		notes = append(m.GetVideoMessage().GetInteractiveAnnotations(), m.GetVideoMessage().GetAnnotations()...)
	case m.GetExtendedTextMessage().GetMusicMetadata() != nil:
		return m.GetExtendedTextMessage().GetMusicMetadata()
	}
	for _, a := range notes {
		if e := a.GetEmbeddedContent().GetEmbeddedMusic(); e != nil {
			return e
		}
	}
	return nil
}

// musicColumn is a song as wz_status.music keeps it; "" without one, or
// when it lacks its artist or title (WhatsApp Web shows none then).
func musicColumn(e *waE2E.EmbeddedMusic) string {
	if e.GetAuthor() == "" || e.GetTitle() == "" {
		return ""
	}
	explicit := ""
	if e.GetIsExplicit() {
		explicit = "1"
	}
	clean := func(s string) string { return strings.ReplaceAll(s, "\n", " ") }
	return clean(e.GetAuthor()) + "\n" + clean(e.GetTitle()) + "\n" + explicit
}

// readMusic fills u's song from wz_status.music.
func readMusic(u *model.StatusUpdate, col string) {
	if col == "" {
		return
	}
	f := strings.SplitN(col, "\n", 3)
	if len(f) < 2 {
		return
	}
	u.Song = &model.Song{Artist: f[0], Title: f[1], Explicit: len(f) > 2 && f[2] == "1"}
}
