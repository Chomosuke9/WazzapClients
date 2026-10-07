package wa

import (
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waHistorySync"
	"github.com/polymorfa/hypermeow/proto/waSyncAction"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"
)

// TestAppStateBeforeHistory checks that pins and mutes a new link's app
// state sync brings before the chats' history are kept, and that the
// history's older copy doesn't undo them.
func TestAppStateBeforeHistory(t *testing.T) {
	b := testBackend(t)
	group := types.NewJID("123", types.GroupServer)
	pinnedAt := time.Unix(1700000000, 0)
	b.handle(&events.Pin{JID: group, Timestamp: pinnedAt, FromFullSync: true,
		Action: &waSyncAction.PinAction{Pinned: proto.Bool(true)}})
	b.handle(&events.Mute{JID: group, FromFullSync: true,
		Action: &waSyncAction.MuteAction{Muted: proto.Bool(true), MuteEndTimestamp: proto.Int64(-1)}})
	if len(b.Chats()) != 0 {
		t.Fatal("a chat without messages is listed")
	}

	b.onHistory(&events.HistorySync{Data: &waHistorySync.HistorySync{
		SyncType: waHistorySync.HistorySync_INITIAL_BOOTSTRAP.Enum(),
		Conversations: []*waHistorySync.Conversation{{
			ID: proto.String(group.String()), Name: proto.String("Team"),
			UnreadCount: proto.Uint32(2), ConversationTimestamp: proto.Uint64(1700000100),
		}},
	}})
	c := b.chat(group.String())
	if c == nil {
		t.Fatal("chat missing after history")
	}
	if !c.Pinned || !c.Muted || c.Unread != 2 || c.Name != "Team" {
		t.Fatalf("pinned %v, muted %v, unread %d, name %q", c.Pinned, c.Muted, c.Unread, c.Name)
	}

	// Unpinned on the phone, then a later history chunk with the old state.
	b.handle(&events.Pin{JID: group, Timestamp: pinnedAt.Add(time.Hour),
		Action: &waSyncAction.PinAction{Pinned: proto.Bool(false)}})
	b.onHistory(&events.HistorySync{Data: &waHistorySync.HistorySync{
		SyncType: waHistorySync.HistorySync_FULL.Enum(),
		Conversations: []*waHistorySync.Conversation{{
			ID: proto.String(group.String()), Pinned: proto.Uint32(uint32(pinnedAt.Unix())),
		}},
	}})
	if c := b.chat(group.String()); c.Pinned {
		t.Fatal("history pinned a chat unpinned since")
	}
}

// TestReadOnPhone checks that messages read on your phone (with read
// receipts on, so the receipt is a plain read from your own device) mark
// the chat read up to them.
func TestReadOnPhone(t *testing.T) {
	b := testBackend(t)
	chat := types.NewJID("456", types.HiddenUserServer)
	for i, id := range []string{"a", "b", "c"} {
		if _, err := b.db.Exec(`INSERT INTO wz_messages (chat, id, sender_jid, ts) VALUES (?, ?, ?, ?)`,
			chat.String(), id, chat.String(), 100+i); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.store.ensureChat(b.ctx, b.db, chat.String(), false, "Ann"); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		_ = b.store.addUnread(b.ctx, chat.String(), false)
	}
	me := types.NewJID("789", types.HiddenUserServer)
	read := func(typ types.ReceiptType, ids ...types.MessageID) {
		b.onReceipt(&events.Receipt{
			MessageSource: types.MessageSource{Chat: chat, Sender: me, IsFromMe: true},
			MessageIDs:    ids, Type: typ, Timestamp: time.Now(),
		})
	}

	read(types.ReceiptTypeDelivered, "a", "b", "c")
	if c := b.chat(chat.String()); c.Unread != 3 {
		t.Fatalf("delivered to the phone: unread %d", c.Unread)
	}
	read(types.ReceiptTypeRead, "a", "b")
	if c := b.chat(chat.String()); c.Unread != 1 {
		t.Fatalf("two of three read on the phone: unread %d", c.Unread)
	}
	read(types.ReceiptTypeReadSelf, "c")
	if c := b.chat(chat.String()); c.Unread != 0 {
		t.Fatalf("all read on the phone: unread %d", c.Unread)
	}

	// A history chunk coming later has the count from when it was made.
	b.onHistory(&events.HistorySync{Data: &waHistorySync.HistorySync{
		SyncType: waHistorySync.HistorySync_FULL.Enum(),
		Conversations: []*waHistorySync.Conversation{{
			ID: proto.String(chat.String()), UnreadCount: proto.Uint32(3),
		}},
	}})
	if c := b.chat(chat.String()); c.Unread != 0 {
		t.Fatalf("history undid the read: unread %d", c.Unread)
	}
}

// TestMarkChatAsRead checks the phone's mark as read and unread, and that a
// full sync's (as old as the chat) leaves the unread count alone.
func TestMarkChatAsRead(t *testing.T) {
	b := testBackend(t)
	chat := types.NewJID("456", types.HiddenUserServer)
	if _, err := b.db.Exec(`INSERT INTO wz_messages (chat, id, sender_jid, ts) VALUES (?, 'a', ?, 100)`,
		chat.String(), chat.String()); err != nil {
		t.Fatal(err)
	}
	_ = b.store.ensureChat(b.ctx, b.db, chat.String(), false, "Ann")
	_ = b.store.addUnread(b.ctx, chat.String(), false)
	mark := func(read, full bool) {
		b.handle(&events.MarkChatAsRead{JID: chat, FromFullSync: full,
			Action: &waSyncAction.MarkChatAsReadAction{Read: proto.Bool(read),
				MessageRange: &waSyncAction.SyncActionMessageRange{LastMessageTimestamp: proto.Int64(50)}}})
	}
	mark(true, true)
	if c := b.chat(chat.String()); c.Unread != 1 {
		t.Fatalf("full sync read: unread %d", c.Unread)
	}
	mark(true, false)
	if c := b.chat(chat.String()); c.Unread != 1 {
		t.Fatalf("read up to an older message: unread %d", c.Unread)
	}
	b.handle(&events.MarkChatAsRead{JID: chat, Action: &waSyncAction.MarkChatAsReadAction{Read: proto.Bool(true)}})
	if c := b.chat(chat.String()); c.Unread != 0 {
		t.Fatalf("read: unread %d", c.Unread)
	}
	mark(false, false)
	if c := b.chat(chat.String()); c.Unread != -1 {
		t.Fatalf("marked unread: unread %d", c.Unread)
	}
	mark(true, false) // opened on the phone
	if c := b.chat(chat.String()); c.Unread != 0 {
		t.Fatalf("read again: unread %d", c.Unread)
	}
}
