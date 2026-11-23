package wa

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	waBinary "github.com/polymorfa/hypermeow/binary"
	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waWeb"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// sealed is a scheduled message holding inner, sealed under key, as
// WhatsApp's phones send it.
func sealed(t *testing.T, inner *waE2E.Message, key []byte, rkid string) *waE2E.Message {
	t.Helper()
	iv := bytes.Repeat([]byte{7}, revealIVLen)
	enc, err := revealSeal(key, iv, marshal(inner))
	if err != nil {
		t.Fatal(err)
	}
	return &waE2E.Message{ConditionalRevealMessage: &waE2E.ConditionalRevealMessage{
		EncPayload: enc, EncIV: iv, RevealKeyID: proto.String(rkid),
		ConditionalRevealMessageType: waE2E.ConditionalRevealMessage_SCHEDULED_MESSAGE.Enum(),
	}}
}

func TestScheduleMeta(t *testing.T) {
	at := time.Unix(1_800_000_000, 0)
	key := bytes.Repeat([]byte{1}, revealKeyLen)
	n := schedMeta("ABC", key, at)
	k := n.GetChildByTag("key")
	if n.Tag != "meta" || n.Attrs["type"] != "scheduled_message" || n.Attrs["st"] != "1800000000" ||
		k.Attrs["rkid"] != "ABC" || !bytes.Equal(k.Content.([]byte), key) {
		t.Fatalf("meta %s", n.String())
	}
	now := time.Date(2026, 11, 23, 15, 0, 30, 0, time.UTC)
	for d, ok := range map[time.Duration]bool{9 * time.Minute: false, 10 * time.Minute: true,
		14 * 24 * time.Hour: true, 14*24*time.Hour + time.Minute: false} {
		if scheduleWindow(now.Truncate(time.Minute).Add(d), now) != ok {
			t.Errorf("%v ahead: taken %v", d, !ok)
		}
	}
}

// Someone's scheduled message shows nothing until its key comes; then it
// joins the chat at its time.
func TestScheduledFromOthers(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	peer := types.NewJID("555", types.HiddenUserServer)
	chat := peer.String()
	key := bytes.Repeat([]byte{9}, revealKeyLen)
	inner := &waE2E.Message{Conversation: proto.String("happy birthday!")}
	info := types.MessageInfo{MessageSource: types.MessageSource{Chat: peer, Sender: peer}, ID: "S1",
		Timestamp: time.Unix(1_700_000_000, 0), PushName: "Rina"}
	b.onMessage(&events.Message{Info: info, Message: sealed(t, inner, key, "K1")})
	if _, ok := b.store.message(ctx, chat, "S1"); ok {
		t.Fatal("a scheduled message shows before its time")
	}
	if len(b.Scheduled("")) != 0 {
		t.Fatal("someone else's scheduled message is listed as yours")
	}
	// The key, however the server hands it out, opens it at its time.
	at := time.Unix(1_700_003_600, 0)
	node := &waBinary.Node{Tag: "message", Attrs: waBinary.Attrs{"id": "S1"}, Content: []waBinary.Node{
		schedMeta("K1", key, at)}}
	b.noteRevealKeys(ctx, node)
	b.revealDue(ctx)
	r, ok := b.store.message(ctx, chat, "S1")
	if !ok || r.Text != "happy birthday!" || r.FromMe || !r.Time.Equal(at) {
		t.Fatalf("revealed %+v (%v)", r.Message, ok)
	}
	var left int
	_ = b.db.QueryRow(`SELECT COUNT(*) FROM wz_scheduled`).Scan(&left)
	if left != 0 {
		t.Errorf("%d still scheduled", left)
	}
}

// One your phone scheduled is the chat's scheduled message (its text
// known once history sync brings its key), and joins the chat at its
// time; taking it back forgets it.
func TestScheduledFromPhone(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	b.clock = func() time.Time { return time.Unix(1_700_000_000, 0) }
	peer := types.NewJID("555", types.HiddenUserServer)
	chat := peer.String()
	me := types.NewJID("111", types.HiddenUserServer)
	key := bytes.Repeat([]byte{3}, revealKeyLen)
	at := time.Unix(1_700_007_200, 0)
	info := func(id string) types.MessageInfo {
		return types.MessageInfo{MessageSource: types.MessageSource{Chat: peer, Sender: me, IsFromMe: true}, ID: id,
			Timestamp: time.Unix(1_700_000_000, 0)}
	}
	msg := sealed(t, &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("see you")}}, key, "K2")
	web := &waWeb.WebMessageInfo{ScheduledMessageMetadata: &waWeb.ScheduledMessageMetadata{
		RevealKeyID: proto.String("K2"), RevealKey: key, ScheduledTime: proto.Uint64(uint64(at.UnixMilli()))}}
	b.onMessage(&events.Message{Info: info("P1"), Message: msg, SourceWebMsg: web})
	list := b.Scheduled(chat)
	if len(list) != 1 || !list[0].Elsewhere || list[0].Shown != "see you" || !list[0].At.Equal(at) {
		t.Fatalf("listed %+v", list)
	}
	// Not yet.
	b.revealDue(ctx)
	if _, ok := b.store.message(ctx, chat, "P1"); ok {
		t.Fatal("shown before its time")
	}
	b.clock = func() time.Time { return at.Add(time.Second) }
	b.revealDue(ctx)
	if r, ok := b.store.message(ctx, chat, "P1"); !ok || r.Text != "see you" || !r.FromMe {
		t.Fatalf("at its time: %+v (%v)", r.Message, ok)
	}

	// Another, taken back from the phone.
	b.clock = func() time.Time { return time.Unix(1_700_000_000, 0) }
	b.onMessage(&events.Message{Info: info("P2"), Message: sealed(t, &waE2E.Message{Conversation: proto.String("x")}, key, "K3")})
	b.onMessage(&events.Message{Info: info("U1"), Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Key:  &waCommon.MessageKey{RemoteJID: proto.String(chat), FromMe: proto.Bool(true), ID: proto.String("P2")},
		Type: waE2E.ProtocolMessage_MESSAGE_UNSCHEDULE.Enum()}}})
	var left int
	_ = b.db.QueryRow(`SELECT COUNT(*) FROM wz_scheduled`).Scan(&left)
	if left != 0 {
		t.Errorf("%d still scheduled after it was taken back", left)
	}
}

// A view once text that came with its text opens here, once; one that
// came without it (what linked devices get) opens on the phone.
func TestViewOnceText(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	peer := types.NewJID("555", types.HiddenUserServer)
	info := types.MessageInfo{MessageSource: types.MessageSource{Chat: peer, Sender: peer}, ID: "T1",
		Timestamp: time.Unix(1_700_000_000, 0)}
	b.onMessage(&events.Message{Info: info, Message: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String("just once"), ViewOnce: proto.Bool(true)}}})
	r, ok := b.store.message(ctx, peer.String(), "T1")
	if !ok || r.Kind != model.KindViewOnce || r.Media != model.MediaNone || r.Text != "just once" || r.OnPhone {
		t.Fatalf("stored %+v", r.Message)
	}
	info.ID = "T2"
	b.handle(&events.UndecryptableMessage{Info: info, IsUnavailable: true, UnavailableType: events.UnavailableTypeViewOnce})
	if r, ok := b.store.message(ctx, peer.String(), "T2"); !ok || r.Kind != model.KindViewOnce || !r.OnPhone {
		t.Fatalf("unavailable %+v", r.Message)
	}
}

// The server hands a scheduled message's recipients its key at its time
// in a mex notification from its sender, as logged from a real one; its
// sender hears whether it went.
func TestScheduledRevealMex(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	peer := types.NewJID("219713397325904", types.HiddenUserServer)
	chat := peer.String()
	key := bytes.Repeat([]byte{0x40, 0xa7}, revealKeyLen/2)
	info := types.MessageInfo{MessageSource: types.MessageSource{Chat: peer, Sender: peer}, ID: "AC5134B8",
		Timestamp: time.Unix(1795423875, 0)}
	b.onMessage(&events.Message{Info: info, Message: sealed(t, &waE2E.Message{Conversation: proto.String("Test")}, key, "ABA91C70")})
	if _, ok := b.store.message(ctx, chat, "AC5134B8"); ok {
		t.Fatal("shown before its key came")
	}
	rk := strings.ReplaceAll(base64.StdEncoding.EncodeToString(key), "/", `\/`) // as the server escapes it
	mex := func(op, json string) *waBinary.Node {
		return &waBinary.Node{Tag: "notification", Attrs: waBinary.Attrs{"from": chat, "type": "mex", "t": "1795424462"},
			Content: []waBinary.Node{{Tag: "update", Attrs: waBinary.Attrs{"op_name": op}, Content: []byte(json)}}}
	}
	b.rawNodes(ctx, mex("NotificationScheduledMessageReveal",
		`{"data":{"xwa2_notify_scheduled_message_reveal":{"chat_jid":"`+chat+`","rk":"`+rk+
			`","rkid":"ABA91C70","sender_user_jid":"`+chat+`","st":1795424460}}}`))
	b.revealDue(ctx)
	r, ok := b.store.message(ctx, chat, "AC5134B8")
	if !ok || r.Text != "Test" || !r.Time.Equal(time.Unix(1795424460, 0)) {
		t.Fatalf("revealed %+v (%v)", r.Message, ok)
	}

	// One of yours the server says didn't go is marked failed.
	_ = b.store.putMessage(ctx, b.db, storedMsg{Message: &model.Message{ID: "MINE", ChatID: chat, FromMe: true,
		Text: "x", Time: time.Unix(1795424460, 0), Receipt: model.Sent}})
	_ = b.putRevealKey(ctx, "MINEKEY", key, 1795424460)
	_, _ = b.db.Exec(`UPDATE wz_reveal_keys SET chat = ?, id = 'MINE' WHERE rkid = 'MINEKEY'`, chat)
	b.rawNodes(ctx, mex("NotificationScheduledMessagePost",
		`{"data":{"xwa2_notify_scheduled_message_post":{"chat_jid":"`+chat+`","error_code":500,"rkid":"MINEKEY","status":"FAILED"}}}`))
	if r, _ := b.store.message(ctx, chat, "MINE"); r.Receipt != model.Failed {
		t.Fatalf("receipt %v after a failed post", r.Receipt)
	}
}

// A photo your phone scheduled lists as a photo, with its caption and
// thumbnail, and shows as one at its time.
func TestScheduledPhotoFromPhone(t *testing.T) {
	b := testBackend(t)
	ctx := b.ctx
	b.clock = func() time.Time { return time.Unix(1_700_000_000, 0) }
	peer := types.NewJID("555", types.HiddenUserServer)
	chat := peer.String()
	me := types.NewJID("111", types.HiddenUserServer)
	key := bytes.Repeat([]byte{5}, revealKeyLen)
	at := time.Unix(1_700_007_200, 0)
	thumb := []byte{0xff, 0xd8, 0xff}
	img := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("sunset"), Mimetype: proto.String("image/jpeg"),
		JPEGThumbnail: thumb, MediaKey: bytes.Repeat([]byte{1}, 32), DirectPath: proto.String("/v/x")}}
	web := &waWeb.WebMessageInfo{ScheduledMessageMetadata: &waWeb.ScheduledMessageMetadata{
		RevealKeyID: proto.String("K9"), RevealKey: key, ScheduledTime: proto.Uint64(uint64(at.UnixMilli()))}}
	info := types.MessageInfo{MessageSource: types.MessageSource{Chat: peer, Sender: me, IsFromMe: true}, ID: "PH1",
		Timestamp: time.Unix(1_700_000_000, 0)}
	b.onMessage(&events.Message{Info: info, Message: sealed(t, img, key, "K9"), SourceWebMsg: web})
	list := b.Scheduled(chat)
	if len(list) != 1 || list[0].Media != model.MediaImage || list[0].Text() != "sunset" || !bytes.Equal(list[0].Thumb, thumb) {
		t.Fatalf("listed %+v", list)
	}
	b.clock = func() time.Time { return at.Add(time.Second) }
	b.revealDue(ctx)
	if r, ok := b.store.message(ctx, chat, "PH1"); !ok || r.Media != model.MediaImage || r.Text != "sunset" {
		t.Fatalf("at its time: %+v (%v)", r.Message, ok)
	}
}
