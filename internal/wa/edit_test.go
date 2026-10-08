package wa

import (
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

// TestEdits checks edits from every path reach the stored message: a live
// edit, one history sync hands out as the new content under the original's
// ID, the original coming again, and an older edit arriving late.
func TestEdits(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	group := types.NewJID("123", types.GroupServer)
	chat := group.String()
	if err := b.store.ensureChat(ctx, b.db, chat, true, "Group"); err != nil {
		t.Fatal(err)
	}
	sent := time.Unix(1_700_000_000, 0)
	info := func(id string, at time.Time) types.MessageInfo {
		return types.MessageInfo{
			MessageSource: types.MessageSource{Chat: group, Sender: types.NewJID("555", types.HiddenUserServer), IsGroup: true},
			ID:            id, Timestamp: at,
		}
	}
	text := func(s string) *waE2E.Message { return &waE2E.Message{Conversation: proto.String(s)} }
	original := &events.Message{Info: info("M1", sent), Message: text("helo")}
	b.onMessage(original)

	stored := func() (string, time.Time) {
		t.Helper()
		r, ok := b.store.message(ctx, chat, "M1")
		if !ok {
			t.Fatal("the message isn't stored")
		}
		return r.Text, r.Edited
	}

	// Live: a protocol message inside an edited message.
	at1 := sent.Add(time.Minute)
	b.onMessage(&events.Message{Info: info("E1", at1), IsEdit: true, Message: &waE2E.Message{
		ProtocolMessage: &waE2E.ProtocolMessage{
			Key:           &waCommon.MessageKey{ID: proto.String("M1")},
			Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
			EditedMessage: text("hello"),
			TimestampMS:   proto.Int64(at1.UnixMilli()),
		}}})
	if txt, ed := stored(); txt != "hello" || !ed.Equal(at1) {
		t.Fatalf("after a live edit: %q edited %v", txt, ed)
	}

	// History sync: the new content under the original's ID.
	at3 := sent.Add(3 * time.Minute)
	b.onMessage(&events.Message{Info: info("M1", at3), IsEdit: true, Message: text("hello all")})
	if txt, ed := stored(); txt != "hello all" || !ed.Equal(at3) {
		t.Fatalf("after a history edit: %q edited %v", txt, ed)
	}

	// The original again (a retry, or history) keeps the edit.
	b.onMessage(original)
	if txt, _ := stored(); txt != "hello all" {
		t.Fatalf("the original coming again undid the edit: %q", txt)
	}

	// An edit older than the last one only joins the versions.
	at2 := sent.Add(2 * time.Minute)
	b.onMessage(&events.Message{Info: info("M1", at2), IsEdit: true, Message: text("hello everyone")})
	if txt, _ := stored(); txt != "hello all" {
		t.Fatalf("a late older edit replaced the text: %q", txt)
	}
	r, _ := b.store.message(ctx, chat, "M1")
	vs := b.Versions(r.Message)
	want := []string{"helo", "hello", "hello everyone"}
	if len(vs) != len(want) {
		t.Fatalf("versions %+v, want %v", vs, want)
	}
	for i, v := range vs {
		if v.Text != want[i] {
			t.Errorf("version %d is %q, want %q", i, v.Text, want[i])
		}
	}
	if !vs[0].Time.Equal(sent) || !vs[1].Time.Equal(at1) {
		t.Errorf("version times %v, %v; want %v, %v", vs[0].Time, vs[1].Time, sent, at1)
	}

	// Deleting it for everyone drops its versions.
	if err := b.store.markDeleted(ctx, chat, "M1", ""); err != nil {
		t.Fatal(err)
	}
	if vs := b.Versions(r.Message); len(vs) != 0 {
		t.Errorf("a deleted message keeps %d versions", len(vs))
	}
}
