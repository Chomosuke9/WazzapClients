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

// TestAdminRevoke checks that a group admin's delete of someone else's
// message records the admin, kept or not, and a sender's own doesn't.
func TestAdminRevoke(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	group := types.NewJID("123", types.GroupServer)
	chat := group.String()
	if err := b.store.ensureChat(ctx, b.db, chat, true, "Group"); err != nil {
		t.Fatal(err)
	}
	author := types.NewJID("555", types.HiddenUserServer)
	admin := types.NewJID("777", types.HiddenUserServer)
	sent := time.Unix(1_700_000_000, 0)
	info := func(id string, from types.JID, edit types.EditAttribute) types.MessageInfo {
		return types.MessageInfo{
			MessageSource: types.MessageSource{Chat: group, Sender: from, IsGroup: true},
			ID:            id, Timestamp: sent, Edit: edit,
		}
	}
	send := func(id string) {
		b.onMessage(&events.Message{Info: info(id, author, ""), Message: &waE2E.Message{Conversation: proto.String("secret " + id)}})
	}
	revoke := func(id string, by types.JID) {
		edit, fromMe := types.EditAttributeSenderRevoke, true
		if by != author {
			edit, fromMe = types.EditAttributeAdminRevoke, false
		}
		b.onMessage(&events.Message{Info: info("R"+id, by, edit), Message: &waE2E.Message{
			ProtocolMessage: &waE2E.ProtocolMessage{
				Key:  &waCommon.MessageKey{ID: proto.String(id), FromMe: proto.Bool(fromMe), Participant: proto.String(author.String())},
				Type: waE2E.ProtocolMessage_REVOKE.Enum(),
			}}})
	}
	stored := func(id string) rawMsg {
		t.Helper()
		r, ok := b.store.message(ctx, chat, id)
		if !ok {
			t.Fatalf("%s isn't stored", id)
		}
		return r
	}

	send("M1")
	revoke("M1", author)
	if r := stored("M1"); r.Kind != model.KindDeleted || r.revokedBy != "" {
		t.Errorf("sender's delete: kind %v by %q", r.Kind, r.revokedBy)
	}
	send("M2")
	revoke("M2", admin)
	r := stored("M2")
	if r.Kind != model.KindDeleted || r.revokedBy != admin.String() {
		t.Errorf("admin's delete: kind %v by %q, want %q", r.Kind, r.revokedBy, admin)
	}
	if m := b.resolve(ctx, r, true); m.DeletedBy == "" || m.DeletedNote() != "This message was deleted by admin "+m.DeletedBy {
		t.Errorf("admin's delete shows %q", m.DeletedNote())
	}

	b.SetPref(model.PrefKeepDeleted, "on")
	send("M3")
	revoke("M3", admin)
	if r := stored("M3"); r.Kind != model.KindText || r.Revoked.IsZero() || r.revokedBy != admin.String() {
		t.Errorf("kept admin's delete: kind %v revoked %v by %q", r.Kind, r.Revoked, r.revokedBy)
	}
}
