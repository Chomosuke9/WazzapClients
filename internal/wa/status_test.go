package wa

import (
	"os"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

func TestStatusQuoteAndExpiry(t *testing.T) {
	b := testBackend(t)
	now := time.Now()
	video := &waE2E.VideoMessage{Seconds: proto.Uint32(12), Caption: proto.String("hi")}
	for _, st := range []storedStatus{
		{id: "vid", sender: "1@lid", ts: now, c: content{media: model.MediaVideo, text: "hi", inner: video}},
		{id: "txt", sender: "1@lid", ts: now, c: content{text: "hello", bg: 0xff112233}},
		{id: "old", sender: "1@lid", ts: now.Add(-3 * statusTTL), c: content{media: model.MediaVideo, inner: video}},
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

func TestPendingStatusDroppedOnStart(t *testing.T) {
	b := testBackend(t)
	now := time.Now()
	for _, id := range []string{"sent", "unsent"} {
		if !b.storeStatus(storedStatus{id: id, sender: "me@lid", fromMe: true, ts: now, c: content{text: id}}) {
			t.Fatal("couldn't store", id)
		}
	}
	// "sent" went out; the app quit while "unsent" was on its way.
	if _, err := b.db.Exec(`DELETE FROM wz_meta WHERE key = ?`, pendingStatus+"sent"); err != nil {
		t.Fatal(err)
	}
	b.dropPendingStatuses(b.ctx)
	threads := b.Statuses()
	if len(threads) != 1 || len(threads[0].Updates) != 1 || threads[0].Updates[0].ID != "sent" {
		t.Fatalf("statuses after restart = %+v", threads)
	}
	if v := b.store.meta(b.ctx, pendingStatus+"unsent"); v != "" {
		t.Errorf("the pending mark stayed: %q", v)
	}
}

// TestKeepDeletedStatus checks that with Keep deleted messages on, a status
// its poster deletes stays and is flagged, and goes with the feature off.
func TestKeepDeletedStatus(t *testing.T) {
	b := testBackend(t)
	now := time.Now()
	poster := types.NewJID("555", types.HiddenUserServer)
	post := func(id string) {
		b.onStatus(&events.Message{
			Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: types.StatusBroadcastJID, Sender: poster},
				ID: types.MessageID(id), Timestamp: now},
			Message: &waE2E.Message{Conversation: proto.String("status " + id)}})
	}
	revoke := func(id string, at time.Time) {
		b.onStatus(&events.Message{
			Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: types.StatusBroadcastJID, Sender: poster},
				ID: types.MessageID("R" + id), Timestamp: at},
			Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
				Key:  &waCommon.MessageKey{ID: proto.String(id)},
				Type: waE2E.ProtocolMessage_REVOKE.Enum(),
			}}})
	}
	updates := func() map[string]*model.StatusUpdate {
		out := map[string]*model.StatusUpdate{}
		for _, th := range b.Statuses() {
			for _, up := range th.Updates {
				out[up.ID] = up
			}
		}
		return out
	}

	post("S1")
	revoke("S1", now.Add(time.Minute))
	if _, ok := updates()["S1"]; ok {
		t.Error("off: the deleted status stayed")
	}

	b.SetPref(model.PrefKeepDeleted, "on")
	at := now.Add(2 * time.Minute).Truncate(time.Millisecond)
	post("S2")
	revoke("S2", at)
	if up := updates()["S2"]; up == nil || up.Text != "status S2" || !up.Revoked.Equal(at) {
		t.Errorf("on: got %+v, want it kept and revoked at %v", up, at)
	}
}
