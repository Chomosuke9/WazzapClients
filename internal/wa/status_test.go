package wa

import (
	"os"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

func TestStatusQuoteAndExpiry(t *testing.T) {
	b := testBackend(t)
	now := time.Now()
	video, _ := proto.Marshal(&waE2E.VideoMessage{Seconds: proto.Uint32(12), Caption: proto.String("hi")})
	for _, st := range []storedStatus{
		{id: "vid", sender: "1@lid", ts: now, c: content{media: model.MediaVideo, text: "hi", blob: video}},
		{id: "txt", sender: "1@lid", ts: now, c: content{text: "hello", bg: 0xff112233}},
		{id: "old", sender: "1@lid", ts: now.Add(-3 * statusTTL), c: content{media: model.MediaVideo, blob: video}},
	} {
		if err := b.store.putStatus(b.ctx, b.db, st); err != nil {
			t.Fatal(err)
		}
	}

	// A reply quotes a video status as the video, and a text status with
	// its background.
	if m := b.quotedMessage(b.ctx, statusChat, "vid"); m.GetVideoMessage().GetSeconds() != 12 {
		t.Errorf("video status quoted as %v", m)
	}
	if m := b.quotedMessage(b.ctx, statusChat, "txt"); m.GetExtendedTextMessage().GetText() != "hello" ||
		m.GetExtendedTextMessage().GetBackgroundArgb() != 0xff112233 {
		t.Errorf("text status quoted as %v", m)
	}
	q := b.quote("1@lid", &model.Message{ID: "vid", ChatID: statusChat, SenderID: "1@lid", Media: model.MediaVideo}, &waE2E.ContextInfo{})
	if q.SenderID != "1@lid" || q.Media != model.MediaVideo {
		t.Errorf("quote = %+v", q)
	}

	// An expired status goes, with its downloaded video.
	old := b.mediaPath(statusChat, "old") + ".mp4"
	_ = os.MkdirAll(b.dataDir+"/media", 0o700)
	if err := os.WriteFile(old, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	threads := b.Statuses()
	if len(threads) != 1 || len(threads[0].Updates) != 2 {
		t.Fatalf("statuses = %+v", threads)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("expired status video kept: %v", err)
	}
}

func TestAudienceOf(t *testing.T) {
	jids := []types.JID{types.NewJID("1", types.DefaultUserServer), types.NewJID("2", types.DefaultUserServer)}
	for _, c := range []struct {
		in   types.StatusPrivacy
		want model.StatusPrivacy
	}{
		{types.StatusPrivacy{Type: types.StatusPrivacyTypeContacts}, model.StatusPrivacy{Audience: model.AudienceContacts}},
		{types.StatusPrivacy{Type: types.StatusPrivacyTypeBlacklist, List: jids}, model.StatusPrivacy{Audience: model.AudienceExcept, Count: 2}},
		{types.StatusPrivacy{Type: types.StatusPrivacyTypeWhitelist, List: jids[:1]}, model.StatusPrivacy{Audience: model.AudienceOnly, Count: 1}},
	} {
		if got := *audienceOf(c.in); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.in.Type, got, c.want)
		}
	}
}
