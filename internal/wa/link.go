package wa

import (
	"encoding/json"

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

// linkInfo is a link preview as stored in the link column; its picture
// is the message's thumb.
type linkInfo struct {
	URL   string `json:"u,omitempty"`
	Title string `json:"t,omitempty"`
	Desc  string `json:"d,omitempty"`
}

func marshalLink(l *model.LinkPreview) string {
	if l == nil {
		return ""
	}
	b, _ := json.Marshal(linkInfo{URL: l.URL, Title: l.Title, Desc: l.Description})
	return string(b)
}

func parseLink(s string) *model.LinkPreview {
	var l linkInfo
	if s == "" || json.Unmarshal([]byte(s), &l) != nil || l.Title == "" && l.Desc == "" {
		return nil
	}
	return &model.LinkPreview{URL: l.URL, Title: l.Title, Description: l.Desc}
}
