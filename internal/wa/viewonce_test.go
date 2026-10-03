package wa

import (
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestViewOnce checks that a view once message that never came to this
// device shows as one that opens on the phone, that a reply quoting it
// brings its media along, and that opening it is kept.
func TestViewOnce(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	peer := types.NewJID("555", types.HiddenUserServer)
	chat := peer.String()
	info := func(id string, at time.Time) types.MessageInfo {
		return types.MessageInfo{MessageSource: types.MessageSource{Chat: peer, Sender: peer}, ID: id, Timestamp: at,
			Type: "media"}
	}
	stored := func(id string) *model.Message {
		t.Helper()
		r, ok := b.store.message(ctx, chat, id)
		if !ok {
			t.Fatalf("%s isn't stored", id)
		}
		return r.Message
	}
	at := time.Unix(1_700_000_000, 0)

	// What linked devices get: no ciphertext at all.
	b.handle(&events.UndecryptableMessage{Info: info("V1", at), IsUnavailable: true,
		UnavailableType: events.UnavailableTypeViewOnce})
	if m := stored("V1"); m.Kind != model.KindViewOnce || !m.OnPhone || m.Opened {
		t.Fatalf("unavailable: kind %v on phone %v opened %v", m.Kind, m.OnPhone, m.Opened)
	}
	// Other undecryptable messages are left to their retry.
	b.handle(&events.UndecryptableMessage{Info: info("U1", at)})
	if _, ok := b.store.message(ctx, chat, "U1"); ok {
		t.Error("a message that failed to decrypt was stored")
	}

	// One that came with its envelope but no key opens on the phone too.
	b.onMessage(&events.Message{Info: info("V2", at), Message: &waE2E.Message{
		ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: &waE2E.Message{
			ImageMessage: &waE2E.ImageMessage{ViewOnce: proto.Bool(true), Caption: proto.String("look")}}}}})
	if m := stored("V2"); m.Kind != model.KindViewOnce || m.Media != model.MediaImage || !m.OnPhone {
		t.Errorf("keyless: kind %v media %v on phone %v", m.Kind, m.Media, m.OnPhone)
	}

	// A reply quotes it with what downloading it takes.
	img := &waE2E.ImageMessage{ViewOnce: proto.Bool(true), MediaKey: []byte{1, 2, 3}, DirectPath: proto.String("/v/t62/x"),
		Mimetype: proto.String("image/jpeg"), Caption: proto.String("secret")}
	b.onMessage(&events.Message{Info: info("R1", at.Add(time.Minute)), Message: &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("lol"), ContextInfo: &waE2E.ContextInfo{
			StanzaID: proto.String("V1"), Participant: proto.String(chat),
			QuotedMessage: &waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{
				Message: &waE2E.Message{ImageMessage: img}}},
		}}}})
	m := stored("V1")
	if m.Kind != model.KindViewOnce || m.OnPhone || m.Media != model.MediaImage || m.Text != "secret" {
		t.Fatalf("after the reply: kind %v on phone %v media %v text %q", m.Kind, m.OnPhone, m.Media, m.Text)
	}
	if media, blob, err := b.store.mediaBlob(ctx, chat, "V1"); err != nil || media != model.MediaImage || len(blob) == 0 {
		t.Errorf("media blob: %v %d bytes, %v", media, len(blob), err)
	}

	b.OpenedViewOnce(m)
	if m := stored("V1"); !m.Opened {
		t.Error("opening it wasn't kept")
	}
	// The message coming again keeps both.
	b.handle(&events.UndecryptableMessage{Info: info("V1", at), IsUnavailable: true,
		UnavailableType: events.UnavailableTypeViewOnce})
	if m := stored("V1"); !m.Opened || m.OnPhone || m.Media != model.MediaImage {
		t.Errorf("again: opened %v on phone %v media %v", m.Opened, m.OnPhone, m.Media)
	}

	// Nor does it show in the Media panel.
	if n, pics := b.store.mediaSummary(ctx, chat, 10); n != 0 || len(pics) != 0 {
		t.Errorf("media summary has %d, %d pictures", n, len(pics))
	}
}
