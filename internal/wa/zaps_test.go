package wa

import (
	"testing"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/store"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	waLog "github.com/polymorfa/hypermeow/util/log"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestZaps checks that Zaps, a photo or a video (a field hypermeow's
// protobufs don't have), come as view once messages with their media,
// and count as opened once played on another device.
func TestZaps(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	me := types.NewJID("111", types.HiddenUserServer)
	phone := types.NewJID("999", types.DefaultUserServer)
	rina := types.NewJID("555", types.HiddenUserServer)
	none := &store.NoopStore{}
	b.cli = whatsmeow.NewClient(&store.Device{ID: &phone, LID: me, Contacts: none, LIDs: none}, waLog.Noop)

	key := []byte("0123456789abcdef0123456789abcdef")
	photo := &waE2E.Message{InstantImageMessage: &waE2E.ImageMessage{Caption: proto.String("hi"),
		MediaKey: key, DirectPath: proto.String("/v/photo"), Mimetype: proto.String("image/jpeg")}}
	vb, err := proto.Marshal(&waE2E.VideoMessage{MediaKey: key, DirectPath: proto.String("/v/video"), Seconds: proto.Uint32(7)})
	if err != nil {
		t.Fatal(err)
	}
	video := &waE2E.Message{}
	video.ProtoReflect().SetUnknown(protowire.AppendBytes(protowire.AppendTag(nil, instantVideoField, protowire.BytesType), vb))
	if instantVideo(video).GetSeconds() != 7 || instantVideo(photo) != nil {
		t.Fatal("instantVideoMessage not read from the unknown fields")
	}

	for i, c := range []struct {
		id    string
		msg   *waE2E.Message
		media model.Media
	}{{"Z1", photo, model.MediaImage}, {"Z2", video, model.MediaVideo}} {
		info := types.MessageInfo{ID: c.id, Timestamp: time.Unix(1000+int64(i), 0),
			MessageSource: types.MessageSource{Chat: rina, Sender: rina}}
		p, ok := b.parse(ctx, &events.Message{Info: info, Message: c.msg, RawMessage: c.msg})
		if !ok || p.msg.Kind != model.KindViewOnce || !p.msg.Zap || p.msg.Media != c.media {
			t.Fatalf("%s: ok %v kind %d zap %v media %v", c.id, ok, p.msg.Kind, p.msg.Zap, p.msg.Media)
		}
		b.apply(&info, p)
	}
	byID := map[string]*model.Message{}
	for _, m := range b.Messages(rina.String(), 10) {
		byID[m.ID] = m
	}
	for _, id := range []string{"Z1", "Z2"} {
		m := byID[id]
		if m == nil || !m.Zap || m.Kind != model.KindViewOnce || m.OnPhone || m.Opened {
			t.Fatalf("%s as stored: %+v", id, m)
		}
		// Its media can be downloaded.
		if _, blob, err := b.store.mediaBlob(ctx, rina.String(), id); err != nil || len(blob) == 0 {
			t.Errorf("%s: no media blob (%v)", id, err)
		}
	}
	if byID["Z2"].Duration != 7 {
		t.Errorf("video's duration = %d", byID["Z2"].Duration)
	}
	if !byID["Z1"].ZapExpired(time.Unix(1000, 0).Add(model.ZapLifetime)) || byID["Z1"].ZapExpired(time.Unix(1000, 0).Add(time.Hour)) {
		t.Error("ZapExpired")
	}

	// Opened on your phone: a played receipt of your own.
	b.onReceipt(&events.Receipt{MessageSource: types.MessageSource{Chat: rina, Sender: me, IsFromMe: true},
		MessageIDs: []types.MessageID{"Z1"}, Type: types.ReceiptTypePlayed, Timestamp: time.Unix(2000, 0)})
	if r, _ := b.store.message(ctx, rina.String(), "Z1"); !r.Opened {
		t.Error("a Zap played on the phone isn't opened")
	}
	if r, _ := b.store.message(ctx, rina.String(), "Z2"); r.Opened {
		t.Error("the other Zap is opened too")
	}
}

// TestZapSend checks the message a Zap goes as: an instantImageMessage, or
// an instantVideoMessage whose copy in the unknown fields follows the
// upload.
func TestZapSend(t *testing.T) {
	photo := &model.Message{Media: model.MediaImage, Kind: model.KindViewOnce, Zap: true, FileType: "image/jpeg"}
	msg, _ := fileMessage(photo, upload{w: 4, h: 3}, "look", nil, false)
	if msg.GetInstantImageMessage().GetCaption() != "look" || msg.GetImageMessage() != nil || msg.GetViewOnceMessageV2() != nil {
		t.Fatalf("photo Zap sent as %v", msg)
	}
	video := &model.Message{Media: model.MediaVideo, Kind: model.KindViewOnce, Zap: true, FileType: "video/mp4", Duration: 5}
	msg, inner := fileMessage(video, upload{}, "", nil, false)
	if msg.GetVideoMessage() != nil || instantVideo(msg).GetSeconds() != 5 {
		t.Fatalf("video Zap sent as %v", msg)
	}
	inner.(*waE2E.VideoMessage).DirectPath = proto.String("/v/uploaded")
	setInstantVideo(msg, inner.(*waE2E.VideoMessage))
	if got := instantVideo(msg); got.GetDirectPath() != "/v/uploaded" || got.GetSeconds() != 5 {
		t.Fatalf("after the upload: %v", got)
	}
	// Once marshaled, as it's sent and stored, it reads back as a Zap.
	var back waE2E.Message
	if err := proto.Unmarshal(marshal(msg), &back); err != nil {
		t.Fatal(err)
	}
	if c := describe(&back); !c.zap || c.media != model.MediaVideo || c.duration != 5 {
		t.Fatalf("read back as %+v", c)
	}
}
