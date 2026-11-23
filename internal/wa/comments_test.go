package wa

import (
	"bytes"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/store"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	waLog "github.com/polymorfa/hypermeow/util/log"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestAnnouncementReplies checks that replies to an announcement, which
// come encrypted, are filed under it and not in the chat, are deleted or
// kept as messages are, and go with it.
func TestAnnouncementReplies(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	group := types.NewJID("123", types.GroupServer)
	chat := group.String()
	author := types.NewJID("555", types.HiddenUserServer)
	me := types.NewJID("111", types.HiddenUserServer)
	phone := types.NewJID("999", types.DefaultUserServer)
	none := &store.NoopStore{}
	cli := func(lid types.JID) *whatsmeow.Client {
		return whatsmeow.NewClient(&store.Device{ID: &phone, LID: lid, Contacts: none, LIDs: none,
			MsgSecrets: secrets{sender: author, secret: bytes.Repeat([]byte{7}, 32)}}, waLog.Noop)
	}
	b.cli = cli(me)
	if err := b.store.ensureChat(ctx, b.db, chat, true, "Announcements"); err != nil {
		t.Fatal(err)
	}
	m := &model.Message{ID: "M", ChatID: chat, SenderID: author.String(), Text: "hello", Time: time.Unix(100, 0)}
	if err := b.store.putMessage(ctx, b.db, storedMsg{Message: m, senderJID: author.String()}); err != nil {
		t.Fatal(err)
	}
	root := &types.MessageInfo{ID: "M", MessageSource: types.MessageSource{Chat: group, Sender: author, IsGroup: true}}
	reply := func(id string, who types.JID, text string, fromMe bool, sender, alt types.JID, at int64) {
		t.Helper()
		msg, err := cli(who).EncryptComment(ctx, root, &waE2E.Message{Conversation: proto.String(text)})
		if err != nil {
			t.Fatal(err)
		}
		b.onMessage(&events.Message{Info: types.MessageInfo{ID: id, Timestamp: time.Unix(at, 0), MessageSource: types.MessageSource{
			Chat: group, Sender: sender, SenderAlt: alt, IsGroup: true, IsFromMe: fromMe}}, Message: msg})
	}
	a, c := types.NewJID("201", types.HiddenUserServer), types.NewJID("202", types.HiddenUserServer)
	reply("R1", a, "first", false, a, types.EmptyJID, 200)
	// Named by phone number, encrypted with the LID.
	reply("R2", c, "second", false, types.NewJID("62802", types.DefaultUserServer), c, 201)
	// Yours, from your phone.
	reply("R3", me, "mine", true, phone, types.EmptyJID, 202)
	reply("R1", a, "first", false, a, types.EmptyJID, 200) // again
	texts := func() (s string) {
		for _, r := range b.Replies(m) {
			s += r.Text + " "
		}
		return s
	}
	if got := texts(); got != "first second mine " {
		t.Fatalf("replies %q", got)
	}
	rs := b.Replies(m)
	if rs[0].Me || rs[0].SenderID != a.String() || !rs[2].Me || rs[2].Sender != "You" || rs[2].Receipt != model.Sent {
		t.Errorf("replies %+v", rs)
	}
	if ms := b.Messages(chat, 10); len(ms) != 1 || ms[0].Replies != 3 {
		t.Fatalf("messages %+v", ms)
	}

	// One that couldn't be decrypted isn't "Waiting for this message".
	b.onUndecryptable(&events.UndecryptableMessage{Info: types.MessageInfo{ID: "R9", MessageSource: types.MessageSource{Chat: group, Sender: a, IsGroup: true},
		MsgMetaInfo: types.MsgMetaInfo{ThreadMessageID: "M"}}})
	if ms := b.Messages(chat, 10); len(ms) != 1 {
		t.Errorf("%d messages after an undecryptable reply", len(ms))
	}

	// History has them decrypted.
	p, ok := b.parse(ctx, &events.Message{Info: types.MessageInfo{ID: "R4", Timestamp: time.Unix(150, 0), MessageSource: types.MessageSource{Chat: group, Sender: a, IsGroup: true}},
		Message: &waE2E.Message{CommentMessage: &waE2E.CommentMessage{TargetMessageKey: &waCommon.MessageKey{ID: proto.String("M")},
			Message: &waE2E.Message{Conversation: proto.String("old")}}}})
	if !ok || p.target != "M" || p.comment == nil || p.comment.id != "R4" {
		t.Errorf("from history: %+v, %v", p, ok)
	}

	revoke := func(id string, by types.JID) {
		b.onMessage(&events.Message{Info: types.MessageInfo{ID: "X" + id, Timestamp: time.Unix(300, 0), MessageSource: types.MessageSource{Chat: group, Sender: by, IsGroup: true}},
			Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{Key: &waCommon.MessageKey{ID: proto.String(id), FromMe: proto.Bool(true)},
				Type: waE2E.ProtocolMessage_REVOKE.Enum()}}})
	}
	revoke("R1", a)
	if got := texts(); got != "second mine " {
		t.Errorf("after deleting the first: %q", got)
	}
	b.SetPref(model.PrefKeepDeleted, "on")
	revoke("R2", c)
	if rs := b.Replies(m); len(rs) != 2 || rs[0].Revoked.IsZero() {
		t.Errorf("kept: %+v", rs)
	}

	// A reply still going when the app closed failed, and goes away alone.
	if err := b.store.putComment(ctx, b.db, chat, "M", comment{id: "R5", who: meVoter, at: time.Unix(400, 0), receipt: model.Pending,
		payload: marshal(&waE2E.Message{Conversation: proto.String("stuck")})}); err != nil {
		t.Fatal(err)
	}
	if err := b.store.failStale(ctx); err != nil {
		t.Fatal(err)
	}
	rs = b.Replies(m)
	if last := rs[len(rs)-1]; last.Receipt != model.Failed {
		t.Fatalf("stuck: %+v", last)
	}
	b.DeleteReply(m, rs[len(rs)-1])
	if got := texts(); got != "second mine " {
		t.Errorf("after deleting the failed one: %q", got)
	}

	if err := b.store.deleteMessage(ctx, chat, "M"); err != nil {
		t.Fatal(err)
	}
	if n := len(b.Replies(m)); n != 0 {
		t.Errorf("%d replies outlived their announcement", n)
	}
}
