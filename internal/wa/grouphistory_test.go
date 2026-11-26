package wa

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"strings"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waGroupHistory"
	"github.com/polymorfa/hypermeow/proto/waWeb"
	"github.com/polymorfa/hypermeow/store"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	waLog "github.com/polymorfa/hypermeow/util/log"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestGroupHistory checks that the messages a group history bundle shares
// come into the group as their authors' (the sharer's own keys were
// written as fromMe), and that the bundle and notice show once each.
func TestGroupHistory(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	group := types.NewJID("123", types.GroupServer)
	other := types.NewJID("456", types.GroupServer)
	me := types.NewJID("111", types.HiddenUserServer)
	phone := types.NewJID("999", types.DefaultUserServer)
	alice := types.NewJID("555", types.HiddenUserServer) // the sharer
	bob := types.NewJID("666", types.HiddenUserServer)
	none := &store.NoopStore{}
	b.cli = whatsmeow.NewClient(&store.Device{ID: &phone, LID: me, Contacts: none, LIDs: none}, waLog.Noop)

	key := func(chat types.JID, id string, fromMe bool, participant types.JID) *waCommon.MessageKey {
		k := &waCommon.MessageKey{RemoteJID: proto.String(chat.String()), ID: proto.String(id), FromMe: proto.Bool(fromMe)}
		if !participant.IsEmpty() {
			k.Participant = proto.String(participant.String())
		}
		return k
	}
	text := func(k *waCommon.MessageKey, ts uint64, s string) *waWeb.WebMessageInfo {
		return &waWeb.WebMessageInfo{Key: k, MessageTimestamp: proto.Uint64(ts),
			Message: &waE2E.Message{Conversation: proto.String(s)}}
	}
	gh := &waGroupHistory.GroupHistory{Messages: []*waWeb.WebMessageInfo{
		text(key(group, "A1", true, types.EmptyJID), 1000, "from alice"),
		text(key(group, "B1", false, bob), 1001, "from bob"),
		text(key(group, "M1", false, me), 1002, "mine"),
		text(key(other, "X1", false, bob), 1003, "another group's"),
		// Bob reacting to Alice's message, its key written from her side.
		{Key: key(group, "B2", false, bob), MessageTimestamp: proto.Uint64(1004), Message: &waE2E.Message{
			ReactionMessage: &waE2E.ReactionMessage{Key: key(group, "A1", true, types.EmptyJID),
				Text: proto.String("👍"), SenderTimestampMS: proto.Int64(1004000)}}},
	}}
	raw, err := proto.Marshal(gh)
	if err != nil {
		t.Fatal(err)
	}
	var z, gz bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write(raw)
	zw.Close()
	gw := gzip.NewWriter(&gz)
	gw.Write(raw)
	gw.Close()
	for _, data := range [][]byte{z.Bytes(), gz.Bytes()} {
		got, err := decodeGroupHistory(data)
		if err != nil || len(got.GetMessages()) != len(gh.GetMessages()) {
			t.Fatalf("decode: %d messages, %v", len(got.GetMessages()), err)
		}
	}
	decoded, _ := decodeGroupHistory(z.Bytes())
	if n := b.injectGroupHistory(ctx, b.cli, group, group, alice, decoded); n != 3 {
		t.Errorf("stored %d messages, want 3", n)
	}
	msgs := b.Messages(group.String(), 10)
	want := map[string]struct {
		sender string
		fromMe bool
	}{"A1": {alice.String(), false}, "B1": {bob.String(), false}, "M1": {"", true}}
	if len(msgs) != len(want) {
		t.Fatalf("messages %v, want %d", ids(msgs), len(want))
	}
	for _, m := range msgs {
		w, ok := want[m.ID]
		if !ok {
			t.Errorf("unexpected message %s (%q)", m.ID, m.Text)
			continue
		}
		if m.FromMe != w.fromMe || !w.fromMe && m.SenderID != w.sender {
			t.Errorf("%s: from %q (me %v), want %q (me %v)", m.ID, m.SenderID, m.FromMe, w.sender, w.fromMe)
		}
		if m.ID == "A1" && (len(m.Reactions) != 1 || m.Reactions[0].Emoji != "👍") {
			t.Errorf("A1's reactions = %v", m.Reactions)
		}
	}
	if c, _ := b.store.chat(ctx, group.String()); c.Unread != 0 {
		t.Errorf("unread = %d", c.Unread)
	}

	// The bundle shows to you, the notice to others; never both.
	md := &waE2E.MessageHistoryMetadata{HistoryReceivers: []string{me.String()},
		OldestMessageTimestampInWindow: proto.Int64(1000)}
	evt := func(id string, m *waE2E.Message) *events.Message {
		return &events.Message{Message: m, Info: types.MessageInfo{ID: id, Timestamp: time.Unix(2000, 0),
			MessageSource: types.MessageSource{Chat: group, Sender: alice, IsGroup: true}}}
	}
	p, ok := b.parse(ctx, evt("H1", &waE2E.Message{MessageHistoryBundle: &waE2E.MessageHistoryBundle{MessageHistoryMetadata: md}}))
	if !ok || p.msg.Kind != model.KindSystem {
		t.Fatalf("bundle for you: %v %+v", ok, p.msg.Message)
	}
	b.apply(&types.MessageInfo{MessageSource: types.MessageSource{IsGroup: true}}, p)
	if _, ok := b.parse(ctx, evt("N1", &waE2E.Message{MessageHistoryNotice: &waE2E.MessageHistoryNotice{MessageHistoryMetadata: md}})); ok {
		t.Error("the notice of a bundle for you shows too")
	}
	toBob := &waE2E.MessageHistoryMetadata{HistoryReceivers: []string{bob.String()}}
	if _, ok := b.parse(ctx, evt("H2", &waE2E.Message{MessageHistoryBundle: &waE2E.MessageHistoryBundle{MessageHistoryMetadata: toBob}})); ok {
		t.Error("a bundle for someone else shows")
	}
	if _, ok := b.parse(ctx, evt("N2", &waE2E.Message{MessageHistoryNotice: &waE2E.MessageHistoryNotice{MessageHistoryMetadata: toBob}})); !ok {
		t.Error("a notice for someone else doesn't show")
	}
	var chip *model.Message
	for _, m := range b.Messages(group.String(), 10) {
		if m.ID == "H1" {
			chip = m
		}
	}
	if chip == nil || !strings.Contains(chip.Text, "shared recent messages since") || !strings.HasSuffix(chip.Text, " with you") {
		t.Errorf("bundle's chip = %+v", chip)
	}
}

func TestHistorySharedText(t *testing.T) {
	object := func(s string) string { return map[string]string{"a": "Alice", "b": "Bob", "me": "you"}[s] }
	for _, c := range []struct {
		actor  string
		params []string
		want   string
	}{
		{"Carol", []string{"0", "me"}, "Carol shared recent messages with you"},
		{"Carol", []string{"0", "a", "b"}, "Carol shared recent messages with Alice and Bob"},
		{"", []string{"0"}, "Recent messages were shared with new members"},
		{"You", nil, "You shared recent messages with new members"},
	} {
		if got := historySharedText(c.actor, c.params, object); got != c.want {
			t.Errorf("%q %v = %q, want %q", c.actor, c.params, got, c.want)
		}
	}
}
