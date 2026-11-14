package wa

import (
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// A group status mention as WhatsApp sends it: a protocol message naming
// the status, whose key is the poster's own.
func TestGroupStatusMention(t *testing.T) {
	b := testBackend(t)
	st := storedStatus{id: "st1", sender: "456@lid", ts: time.Now(), c: content{media: model.MediaImage, thumb: []byte("jpeg"),
		inner: &waE2E.ImageMessage{}}}
	if err := b.store.putStatus(b.ctx, b.db, st); err != nil {
		t.Fatal(err)
	}
	msg := &waE2E.Message{GroupStatusMentionMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{
		ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_STATUS_MENTION_MESSAGE.Enum(),
			Key:  &waCommon.MessageKey{RemoteJID: proto.String(statusChat), FromMe: proto.Bool(true), ID: proto.String("st1"), Participant: proto.String("status_me")},
		}}}}
	b.onMessage(snippetEvent("men", msg))

	r, ok := b.store.message(b.ctx, "123@g.us", "men")
	if !ok || r.Kind != model.KindText || r.StatusMention == nil {
		t.Fatalf("stored as %+v", r.Message)
	}
	m := b.resolve(b.ctx, r, true)
	sm := m.StatusMention
	if sm.StatusID != "st1" || !sm.Group || !sm.Found || sm.Media != model.MediaImage || string(sm.Thumb) != "jpeg" {
		t.Fatalf("mention = %+v", sm)
	}
	if k := b.store.statusMentions(b.ctx)["st1"]; k != [2]string{"123@g.us", "men"} {
		t.Fatalf("mention of st1 = %v", k)
	}

	// Its status deleted, the card says nothing of it and the viewer
	// forgets the mention.
	if err := b.store.putStatus(b.ctx, b.db, storedStatus{revoke: "st1"}); err != nil {
		t.Fatal(err)
	}
	r, _ = b.store.message(b.ctx, "123@g.us", "men")
	if m := b.resolve(b.ctx, r, true); m.StatusMention.Found {
		t.Error("deleted status still found")
	}
	if _, ok := b.store.statusMentions(b.ctx)["st1"]; ok {
		t.Error("deleted status's mention kept")
	}
}

// A like of your status (a reaction to it) shows beside its viewer, and
// your own like from your phone marks the status liked.
func TestStatusReactions(t *testing.T) {
	b := testBackend(t)
	now := time.Now()
	for _, st := range []storedStatus{
		{id: "mine", sender: "me@lid", fromMe: true, ts: now, c: content{text: "hi"}},
		{id: "theirs", sender: "456@lid", participant: "456@lid", ts: now, c: content{text: "yo"}},
	} {
		if err := b.store.putStatus(b.ctx, b.db, st); err != nil {
			t.Fatal(err)
		}
	}
	react := func(id string, fromMe bool, sender types.JID, key string) {
		e := &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: types.StatusBroadcastJID, Sender: sender, IsFromMe: fromMe},
			ID: "r-" + id, Timestamp: now}, Message: &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{
			Key:  &waCommon.MessageKey{RemoteJID: proto.String(statusChat), ID: proto.String(id), Participant: proto.String(key)},
			Text: proto.String(statusLike), SenderTimestampMS: proto.Int64(now.UnixMilli())}}}
		b.onMessage(e)
	}
	react("mine", false, types.NewJID("789", types.HiddenUserServer), "me@lid")
	v := b.StatusViews("mine")
	if len(v.Viewers) != 1 || v.Viewers[0].ID != "789@lid" || v.Viewers[0].Reaction != statusLike {
		t.Fatalf("viewers = %+v", v.Viewers)
	}
	react("theirs", true, types.NewJID("me", types.HiddenUserServer), "456@lid")
	if !b.store.statusLikes(b.ctx)["theirs"] {
		t.Error("your like from another device not kept")
	}
}
