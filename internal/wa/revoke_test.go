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

// TestKeepDeleted checks that with Keep deleted messages on, a message
// someone deletes for everyone keeps its text and is flagged, while your
// own deletes and deletes with the feature off still remove it.
func TestKeepDeleted(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	group := types.NewJID("123", types.GroupServer)
	chat := group.String()
	if err := b.store.ensureChat(ctx, b.db, chat, true, "Group"); err != nil {
		t.Fatal(err)
	}
	sent := time.Unix(1_700_000_000, 0)
	info := func(id string, at time.Time, fromMe bool) types.MessageInfo {
		return types.MessageInfo{
			MessageSource: types.MessageSource{Chat: group, Sender: types.NewJID("555", types.HiddenUserServer),
				IsGroup: true, IsFromMe: fromMe},
			ID: id, Timestamp: at,
		}
	}
	send := func(id string, fromMe bool) {
		b.onMessage(&events.Message{Info: info(id, sent, fromMe), Message: &waE2E.Message{Conversation: proto.String("secret " + id)}})
	}
	revoke := func(id string, at time.Time, fromMe bool) {
		b.onMessage(&events.Message{Info: info("R"+id, at, fromMe), Message: &waE2E.Message{
			ProtocolMessage: &waE2E.ProtocolMessage{
				Key:  &waCommon.MessageKey{ID: proto.String(id)},
				Type: waE2E.ProtocolMessage_REVOKE.Enum(),
			}}})
	}
	stored := func(id string) *model.Message {
		t.Helper()
		r, ok := b.store.message(ctx, chat, id)
		if !ok {
			t.Fatalf("%s isn't stored", id)
		}
		return r.Message
	}

	// Off: the message turns into "This message was deleted".
	send("M1", false)
	revoke("M1", sent.Add(time.Minute), false)
	if m := stored("M1"); m.Kind != model.KindDeleted || m.Text != "" || !m.Revoked.IsZero() {
		t.Errorf("off: kind %v text %q revoked %v", m.Kind, m.Text, m.Revoked)
	}

	b.SetPref(model.PrefKeepDeleted, "on")
	at := sent.Add(2 * time.Minute)
	send("M2", false)
	revoke("M2", at, false)
	if m := stored("M2"); m.Kind != model.KindText || m.Text != "secret M2" || !m.Revoked.Equal(at) {
		t.Errorf("on: kind %v text %q revoked %v, want revoked %v", m.Kind, m.Text, m.Revoked, at)
	}
	// The message coming again (a retry) stays flagged.
	send("M2", false)
	if m := stored("M2"); !m.Revoked.Equal(at) {
		t.Errorf("the message again cleared revoked: %v", m.Revoked)
	}

	// Your own delete, from another device, still removes it.
	send("M3", true)
	revoke("M3", at, true)
	if m := stored("M3"); m.Kind != model.KindDeleted {
		t.Errorf("own delete kept the message: kind %v text %q", m.Kind, m.Text)
	}
}
