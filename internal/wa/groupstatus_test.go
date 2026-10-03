package wa

import (
	"os"
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// These fixtures match the supplied V2 envelopes, without live media keys.
func TestGroupStatusPayloads(t *testing.T) {
	cases := []struct {
		name, payload string
		media         model.Media
	}{
		{"text", `{"groupStatusMessageV2":{"message":{"extendedTextMessage":{"text":"Tesy","textArgb":4294967295,"backgroundArgb":4289080433,"contextInfo":{"isGroupStatus":true}}}}}`, model.MediaNone},
		{"image", `{"groupStatusMessageV2":{"message":{"imageMessage":{"caption":"This is a caption","mimetype":"image/jpeg","directPath":"/test","contextInfo":{"isGroupStatus":true}}}}}`, model.MediaImage},
		{"voice", `{"groupStatusMessageV2":{"message":{"audioMessage":{"mimetype":"audio/ogg; codecs=opus","seconds":1,"PTT":true,"directPath":"/test","contextInfo":{"isGroupStatus":true}}}}}`, model.MediaVoice},
		{"video", `{"groupStatusMessageV2":{"message":{"videoMessage":{"mimetype":"video/mp4","seconds":12,"directPath":"/test","contextInfo":{"isGroupStatus":true}}}}}`, model.MediaVideo},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := testBackend(t)
			var msg waE2E.Message
			if err := protojson.Unmarshal([]byte(tc.payload), &msg); err != nil {
				t.Fatal(err)
			}
			evt := &events.Message{Info: types.MessageInfo{ID: tc.name, Timestamp: time.Now(), MessageSource: types.MessageSource{Chat: types.NewJID("123", types.GroupServer), Sender: types.NewJID("456", types.HiddenUserServer), IsGroup: true}}, Message: &msg}
			b.onMessage(evt)
			threads := b.Statuses()
			if len(threads) != 1 || !threads[0].Group || threads[0].ID != "123@g.us" || threads[0].Mine {
				t.Fatalf("group threads: %+v", threads)
			}
			up := threads[0].Last()
			if up.Media != tc.media || up.SenderID != "456@lid" {
				t.Fatalf("update: %+v", up)
			}
			if tc.name == "text" && (up.Text != "Tesy" || up.Background != 4289080433) {
				t.Fatalf("text: %+v", up)
			}
			if tc.name == "voice" && (up.Duration != 1 || up.FileType != "audio/ogg; codecs=opus") {
				t.Fatalf("voice: %+v", up)
			}
			var n int
			if err := b.db.QueryRow(`SELECT count(*) FROM wz_messages`).Scan(&n); err != nil || n != 0 {
				t.Fatalf("status leaked to chat: %d %v", n, err)
			}
			if tc.media != model.MediaNone {
				media, blob, err := b.store.mediaBlob(b.ctx, statusChat, tc.name)
				if err != nil || media != tc.media || len(blob) == 0 {
					t.Fatalf("download metadata missing: %v", err)
				}
			}
			b.ViewStatus(threads[0].ID, up.ID)
			b.onMessage(evt) // repeat/history sync must retain viewed state
			if !b.Statuses()[0].Last().Viewed {
				t.Fatal("duplicate reset viewed state")
			}
			normal := *evt
			normal.Message = unwrap(&msg)
			if isGroupStatus(&normal) {
				t.Fatal("ordinary/quoted media misclassified")
			}
			normal.Info.Chat = types.NewJID("456", types.HiddenUserServer)
			normal.Message = &msg
			if isGroupStatus(&normal) {
				t.Fatal("direct message misclassified as group status")
			}
		})
	}
}

func TestGroupStatusSeparationAndExpiry(t *testing.T) {
	b := testBackend(t)
	now := time.Now()
	for _, st := range []storedStatus{
		{id: "personal", sender: "1@lid", ts: now, c: content{text: "personal"}},
		{id: "a", group: "123@g.us", sender: "1@lid", ts: now, c: content{text: "a"}},
		{id: "b", group: "123@g.us", sender: "2@lid", fromMe: true, ts: now, c: content{text: "b"}},
		{id: "c", group: "789@g.us", sender: "1@lid", ts: now, c: content{text: "c"}},
		{id: "old", group: "123@g.us", sender: "1@lid", ts: now.Add(-25 * time.Hour), c: content{media: model.MediaVoice}},
	} {
		if err := b.store.putStatus(b.ctx, b.db, st); err != nil {
			t.Fatal(err)
		}
	}
	path := b.mediaPath(statusChat, "old") + ".ogg"
	if err := os.MkdirAll(b.dataDir+"/media", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("expired"), 0600); err != nil {
		t.Fatal(err)
	}
	threads := b.Statuses()
	if len(threads) != 3 {
		t.Fatalf("got %d threads", len(threads))
	}
	for _, thread := range threads {
		if thread.ID == "123@g.us" && (len(thread.Updates) != 2 || !thread.Updates[1].FromMe || !thread.Updates[1].Viewed) {
			t.Fatalf("own group update: %+v", thread)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expired audio cache retained")
	}
}

func TestOutgoingGroupStatusEnvelope(t *testing.T) {
	for _, inner := range []*waE2E.Message{
		{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("hello"), BackgroundArgb: proto.Uint32(0xff123456)}},
		{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("caption"), MediaKey: []byte{1, 2}}},
		{VideoMessage: &waE2E.VideoMessage{Seconds: proto.Uint32(12)}},
		{AudioMessage: &waE2E.AudioMessage{PTT: proto.Bool(true), Seconds: proto.Uint32(1)}},
	} {
		wrapped, extra := groupStatusMessage(inner)
		if wrapped.GetGroupStatusMessageV2().GetMessage() != inner || !describe(wrapped).ctx.GetIsGroupStatus() {
			t.Fatal("missing V2 or context flag")
		}
		if len(wrapped.GetMessageContextInfo().GetMessageSecret()) != 32 {
			t.Fatal("missing message secret")
		}
		if extra.AdditionalNodes == nil || len(*extra.AdditionalNodes) != 1 || (*extra.AdditionalNodes)[0].Attrs["is_group_status"] != "true" {
			t.Fatal("missing group status node")
		}
		raw, err := proto.Marshal(wrapped)
		if err != nil {
			t.Fatal(err)
		}
		var decoded waE2E.Message
		if err := proto.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(&decoded, wrapped) {
			t.Fatal("wire round trip changed payload")
		}
	}
}

func TestGroupStatusRevokeAndReceipt(t *testing.T) {
	b := testBackend(t)
	group := types.NewJID("123", types.GroupServer)
	if err := b.store.ensureChat(b.ctx, b.db, group.String(), true, "Test group"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.db.Exec(`UPDATE wz_chats SET unread = 4 WHERE jid = ?`, group.String()); err != nil {
		t.Fatal(err)
	}
	st := storedStatus{id: "story", group: group.String(), sender: "456@lid", ts: time.Now(), c: content{text: "hello"}}
	if err := b.store.putStatus(b.ctx, b.db, st); err != nil {
		t.Fatal(err)
	}
	b.onReceipt(&events.Receipt{MessageSource: types.MessageSource{Chat: group}, MessageIDs: []types.MessageID{"story"}, Type: types.ReceiptTypeReadSelf})
	if !b.Statuses()[0].Last().Viewed {
		t.Fatal("other device view not synchronized")
	}
	if c, ok := b.store.chat(b.ctx, group.String()); !ok || c.Unread != 4 {
		t.Fatal("reading a status cleared unread chat messages")
	}
	b.onMessage(&events.Message{Info: types.MessageInfo{ID: "delete", Timestamp: time.Now(), MessageSource: types.MessageSource{Chat: group, Sender: types.NewJID("456", types.HiddenUserServer)}}, Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Type: waE2E.ProtocolMessage_REVOKE.Enum(), Key: &waCommon.MessageKey{ID: proto.String("story")}}}})
	if len(b.Statuses()) != 0 {
		t.Fatal("deleted group status retained")
	}
}
