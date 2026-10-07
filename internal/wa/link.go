package wa

import (
	"context"
	"time"

	whatsmeow "github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// addLink puts a link preview, and its picture, on a message to send.
func addLink(e *waE2E.ExtendedTextMessage, l *model.LinkPreview, thumb []byte) {
	e.MatchedText = proto.String(l.URL)
	if l.Title != "" {
		e.Title = proto.String(l.Title)
	}
	if l.Description != "" {
		e.Description = proto.String(l.Description)
	}
	e.PreviewType = waE2E.ExtendedTextMessage_NONE.Enum()
	if len(thumb) > 0 {
		e.JPEGThumbnail = thumb
	}
}

// uploadLinkImage uploads a link preview's big picture and points e at
// it, as WhatsApp does: other devices then show the wide card. Without it
// (an upload that fails) the message still goes, with the small picture.
func (b *Backend) uploadLinkImage(ctx context.Context, cli *whatsmeow.Client, e *waE2E.ExtendedTextMessage, img model.LinkImage) {
	res, err := cli.Upload(ctx, img.Data, whatsmeow.MediaLinkThumbnail)
	if err != nil {
		b.log.Warnf("upload link preview picture: %v", err)
		e.ThumbnailWidth, e.ThumbnailHeight = nil, nil
		return
	}
	e.ThumbnailDirectPath = proto.String(res.DirectPath)
	e.ThumbnailSHA256, e.ThumbnailEncSHA256, e.MediaKey = res.FileSHA256, res.FileEncSHA256, res.MediaKey
	e.MediaKeyTimestamp = proto.Int64(time.Now().Unix())
	e.ThumbnailWidth, e.ThumbnailHeight = proto.Uint32(uint32(img.W)), proto.Uint32(uint32(img.H))
}

// linkImageOf returns where a link preview's big picture is, for
// downloading it (the message's media blob), or nil when the preview has
// none.
func linkImageOf(e *waE2E.ExtendedTextMessage) proto.Message {
	if e.GetThumbnailDirectPath() == "" || len(e.GetMediaKey()) == 0 || e.GetThumbnailWidth() == 0 || e.GetThumbnailHeight() == 0 {
		return nil
	}
	return &waE2E.ExtendedTextMessage{
		ThumbnailDirectPath: e.ThumbnailDirectPath, ThumbnailSHA256: e.ThumbnailSHA256,
		ThumbnailEncSHA256: e.ThumbnailEncSHA256, MediaKey: e.MediaKey, MediaKeyTimestamp: e.MediaKeyTimestamp,
		ThumbnailWidth: e.ThumbnailWidth, ThumbnailHeight: e.ThumbnailHeight,
	}
}

// copyLinkImage points e at the big picture in blob (from linkImageOf),
// for forwarding a link preview.
func copyLinkImage(e *waE2E.ExtendedTextMessage, blob []byte) {
	var s waE2E.ExtendedTextMessage
	if len(blob) == 0 || proto.Unmarshal(blob, &s) != nil || s.GetThumbnailDirectPath() == "" {
		return
	}
	e.ThumbnailDirectPath, e.ThumbnailSHA256, e.ThumbnailEncSHA256 = s.ThumbnailDirectPath, s.ThumbnailSHA256, s.ThumbnailEncSHA256
	e.MediaKey, e.MediaKeyTimestamp = s.MediaKey, s.MediaKeyTimestamp
	e.ThumbnailWidth, e.ThumbnailHeight = s.ThumbnailWidth, s.ThumbnailHeight
}
