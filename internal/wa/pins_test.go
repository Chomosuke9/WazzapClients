package wa

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waWeb"
	"github.com/polymorfa/hypermeow/store"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	waLog "github.com/polymorfa/hypermeow/util/log"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestPins checks a chat keeps its latest pins, the latest first, that one
// ends when it says, and that history's order doesn't matter.
func TestPins(t *testing.T) {
	b := testBackend(t)
	group := types.NewJID("123", types.GroupServer)
	chat := group.String()
	sender := types.NewJID("555", types.HiddenUserServer)
	now := time.Now()
	event := func(id string, at time.Time, m *waE2E.Message) *events.Message {
		return &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: group, Sender: sender, IsGroup: true},
			ID: id, Timestamp: at}, Message: m}
	}
	pin := func(id string, at time.Time, secs uint32, on bool) {
		typ := waE2E.PinInChatMessage_PIN_FOR_ALL
		if !on {
			typ = waE2E.PinInChatMessage_UNPIN_FOR_ALL
		}
		b.onMessage(event("P"+id+at.String(), at, &waE2E.Message{
			PinInChatMessage: &waE2E.PinInChatMessage{Key: &waCommon.MessageKey{ID: proto.String(id)}, Type: typ.Enum(),
				SenderTimestampMS: proto.Int64(at.UnixMilli())},
			MessageContextInfo: &waE2E.MessageContextInfo{MessageAddOnDurationInSecs: proto.Uint32(secs)},
		}))
	}
	pinned := func() string {
		s := ""
		for _, m := range b.PinnedMessages(chat) {
			if !m.Pinned {
				t.Errorf("%s isn't marked pinned", m.ID)
			}
			s += m.ID
		}
		return s
	}
	for i, id := range []string{"a", "b", "c", "d", "e"} {
		b.onMessage(event(id, now.Add(-time.Hour+time.Duration(i)*time.Second), &waE2E.Message{Conversation: proto.String(id)}))
	}
	for i, id := range []string{"a", "b", "c", "d", "e"} {
		pin(id, now.Add(time.Duration(i-10)*time.Minute), 7*24*3600, true)
	}
	if got := pinned(); got != "edcb" {
		t.Errorf("pinned %q", got)
	}
	pin("d", now, 0, false)
	if got := pinned(); got != "ecb" {
		t.Errorf("after unpinning d: %q", got)
	}
	pin("a", now.Add(-time.Hour), 60, true) // ended
	if got := pinned(); got != "ecb" {
		t.Errorf("a pin that ended shows: %q", got)
	}
	pin("a", now, 0, true) // for good
	if got := pinned(); got != "aecb" {
		t.Errorf("after pinning a: %q", got)
	}
	if r, _ := b.store.message(b.ctx, chat, "d"); r.Pinned {
		t.Error("d is still marked pinned")
	}

	w := &waWeb.WebMessageInfo{PinInChat: &waWeb.PinInChat{Type: waWeb.PinInChat_PIN_FOR_ALL.Enum(),
		SenderTimestampMS: proto.Int64(5000), MessageAddOnContextInfo: &waWeb.MessageAddOnContextInfo{MessageAddOnDurationInSecs: proto.Uint32(10)}}}
	if p, ok := historyPin(w, "x"); !ok || p.pin != 1 || p.pinAt != 5000 || p.pinUntil != 15 || p.target != "x" {
		t.Errorf("history pin %+v, %v", p, ok)
	}
	if _, ok := historyPin(&waWeb.WebMessageInfo{}, "x"); ok {
		t.Error("a message without a pin has one")
	}
}

// TestAlbumReplies checks that a reply to a whole album names it, and that
// what is done to the album's own ID shows on its first picture.
func TestAlbumReplies(t *testing.T) {
	b := testBackend(t)
	group := types.NewJID("123", types.GroupServer)
	chat := group.String()
	sender := types.NewJID("555", types.HiddenUserServer)
	at := time.Unix(1_700_000_000, 0)
	put := func(id string, m *waE2E.Message) {
		at = at.Add(time.Second)
		b.onMessage(&events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: group, Sender: sender, IsGroup: true},
			ID: id, Timestamp: at}, Message: m})
	}
	for _, id := range []string{"p1", "p2"} {
		m := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg")}}
		inAlbum(m, group, "ALBUM1")
		put(id, m)
	}
	put("r", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("nice"), ContextInfo: &waE2E.ContextInfo{
		StanzaID: proto.String("ALBUM1"), Participant: proto.String(sender.String()),
		QuotedMessage: &waE2E.Message{AlbumMessage: &waE2E.AlbumMessage{ExpectedImageCount: proto.Uint32(2)}},
	}}})
	// A picture that replies to the album holds its ID without being in it.
	put("r2", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("ALBUM1"),
		QuotedMessage: &waE2E.Message{AlbumMessage: &waE2E.AlbumMessage{}}}}})
	var reply *model.Message
	for _, m := range b.Messages(chat, 10) {
		if m.ID == "r" {
			reply = m
		}
	}
	if reply == nil || reply.Quote == nil {
		t.Fatalf("reply %+v", reply)
	}
	if q := reply.Quote; !q.Album || q.ID != "ALBUM1" || q.Text != "2 photos" || q.Media != model.MediaImage {
		t.Errorf("quote %+v", q)
	}
	// A reply of yours to all of it quotes it the same way.
	first := *b.Messages(chat, 10)[0]
	first.Whole = &model.AlbumSize{Photos: 2}
	ci := &waE2E.ContextInfo{}
	q := b.quote(chat, &first, ci)
	if a := ci.GetQuotedMessage().GetAlbumMessage(); ci.GetStanzaID() != "ALBUM1" || ci.GetParticipant() != sender.String() ||
		a.GetExpectedImageCount() != 2 || !q.Album || q.ID != "ALBUM1" || q.Text != "2 photos" {
		t.Errorf("quoted as %v, shown %+v", ci, q)
	}
	if rq := quoteOf(ci); !rq.album || rq.id != "ALBUM1" || rq.text != "2 photos" {
		t.Errorf("read back as %+v", rq)
	}
	if got := b.AlbumMessage(chat, "ALBUM1"); got != "p1" {
		t.Errorf("the album's first message is %q", got)
	}
	if got := b.AlbumMessage(chat, "NONE"); got != "" {
		t.Errorf("an unknown album's first message is %q", got)
	}
	put("x", &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Key: &waCommon.MessageKey{ID: proto.String("ALBUM1")}, Text: proto.String("👍")}})
	if r, _ := b.store.message(b.ctx, chat, "p1"); len(r.Reactions) != 1 || r.Reactions[0].Emoji != "👍" {
		t.Errorf("reactions on the first picture: %+v", r.Reactions)
	}
}

// TestStatusQuote checks that a reply to a status update says so.
func TestStatusQuote(t *testing.T) {
	b := testBackend(t)
	q := b.resolveQuote(context.Background(), "555@lid", quoteOf(&waE2E.ContextInfo{StanzaID: proto.String("S1"), RemoteJID: proto.String(statusChat),
		Participant: proto.String("555@lid"), QuotedMessage: &waE2E.Message{Conversation: proto.String("my status")}}))
	if q == nil || !q.Status || q.ID != "S1" {
		t.Errorf("quote %+v", q)
	}
}

// TestKeepDeletedChannel checks that with Keep deleted messages on, a
// channel's deleted post keeps its content and is flagged, your own
// channel's too.
func TestKeepDeletedChannel(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	del := func(chat types.JID, id string) *model.Message {
		t.Helper()
		_ = b.store.ensureChat(ctx, b.db, chat.String(), false, "Ch")
		if err := b.store.putMessage(ctx, b.db, storedMsg{Message: &model.Message{ID: id, ChatID: chat.String(), Text: "news", Time: time.Unix(100, 0)}}); err != nil {
			t.Fatal(err)
		}
		b.onMessage(&events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: chat, Sender: chat}, ID: id,
			Timestamp: time.Unix(200, 0), Edit: types.EditAttributeAdminRevoke}, Message: &waE2E.Message{}})
		r, _ := b.store.message(ctx, chat.String(), id)
		return r.Message
	}
	ch := types.NewJID("120363000000000001", types.NewsletterServer)
	if m := del(ch, "p1"); m.Kind != model.KindDeleted {
		t.Errorf("off: kind %v", m.Kind)
	}
	b.SetPref(model.PrefKeepDeleted, "on")
	if m := del(ch, "p2"); m.Kind != model.KindText || m.Text != "news" || !m.Revoked.Equal(time.Unix(200, 0)) {
		t.Errorf("on: kind %v text %q revoked %v", m.Kind, m.Text, m.Revoked)
	}
	own := types.NewJID("120363000000000002", types.NewsletterServer)
	b.storeChannel(ctx, channelMeta{jid: own.String(), name: "Mine", following: true, owner: true})
	if m := del(own, "p3"); m.Kind != model.KindText || m.Revoked.IsZero() {
		t.Errorf("your own channel: kind %v revoked %v", m.Kind, m.Revoked)
	}
}

// secrets is the one message secret a test's device store holds.
type secrets struct {
	store.MsgSecretStore
	sender types.JID
	secret []byte
}

func (s secrets) GetMessageSecret(context.Context, types.JID, types.JID, types.MessageID) ([]byte, types.JID, error) {
	return s.secret, s.sender, nil
}

// TestAnnouncementReactions checks that a reaction in a community's
// announcements, which comes encrypted, counts, and that yours goes out
// that way.
func TestAnnouncementReactions(t *testing.T) {
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
	if b.store.announces(ctx, chat) {
		t.Error("announces before it's known to")
	}
	_, _ = b.db.ExecContext(ctx, `UPDATE wz_chats SET announce_sub = 1 WHERE jid = ?`, chat)
	if !b.store.announces(ctx, chat) {
		t.Error("doesn't announce")
	}
	m := &model.Message{ID: "M", ChatID: chat, SenderID: author.String(), Text: "hello", Time: time.Unix(100, 0)}
	if err := b.store.putMessage(ctx, b.db, storedMsg{Message: m, senderJID: author.String()}); err != nil {
		t.Fatal(err)
	}
	for i, user := range []string{"201", "202", "203"} {
		who := types.NewJID(user, types.HiddenUserServer)
		// As their client sends it.
		msg := b.encryptReaction(ctx, cli(who), group, m, cli(who).BuildReaction(group, author, "M", "🔥"))
		if msg.GetEncReactionMessage() == nil || msg.GetReactionMessage() != nil {
			t.Fatalf("not encrypted: %+v", msg)
		}
		info := types.MessageInfo{MessageSource: types.MessageSource{Chat: group, Sender: who, IsGroup: true}, ID: "R" + user, Timestamp: time.Unix(200, 0)}
		if i == 2 {
			// By phone number, with the LID it's encrypted with beside it.
			info.Sender, info.SenderAlt = types.NewJID("62803", types.DefaultUserServer), who
		}
		b.onMessage(&events.Message{Info: info, Message: msg})
	}
	r, _ := b.store.message(ctx, chat, "M")
	if len(r.Reactions) != 1 || r.Reactions[0] != (model.ReactionCount{Emoji: "🔥", Count: 3}) {
		t.Errorf("reactions %+v", r.Reactions)
	}
}
