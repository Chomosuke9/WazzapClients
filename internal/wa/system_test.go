package wa

import (
	"strings"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waWeb"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// TestGroupSystemMessages checks that a group's changes show in the chat
// as system messages, worded with the names known when they're read, and
// that the same change coming again shows once.
func TestGroupSystemMessages(t *testing.T) {
	b := testBackend(t)
	g := types.NewJID("123", types.GroupServer)
	alice, bob := types.NewJID("111", types.HiddenUserServer), types.NewJID("222", types.HiddenUserServer)
	at := time.Unix(1_700_000_000, 0)
	e := &events.GroupInfo{JID: g, Sender: &alice, Timestamp: at, Join: []types.JID{bob},
		Name: &types.GroupName{Name: "Weekend"}}
	b.onGroupSystem(b.ctx, e)
	b.onGroupSystem(b.ctx, e) // again
	b.onGroupSystem(b.ctx, &events.GroupInfo{JID: g, Sender: &bob, Timestamp: at.Add(time.Minute), Leave: []types.JID{bob}})
	b.onGroupSystem(b.ctx, &events.GroupInfo{JID: g, Sender: &alice, Timestamp: at.Add(2 * time.Minute),
		Ephemeral: &types.GroupEphemeral{IsEphemeral: true, DisappearingTimer: 86400}})
	msgs := b.Messages(g.String(), 10)
	var got []string
	for _, m := range msgs {
		if m.Kind != model.KindSystem {
			t.Errorf("%s: kind %d, want system", m.Text, m.Kind)
		}
		got = append(got, m.Text)
	}
	want := []string{
		`changed the group name to "Weekend"`,
		" added ",
		" left",
		"turned on disappearing messages. All new messages will disappear from this chat 24 hours after they're sent.",
	}
	if len(got) != len(want) {
		t.Fatalf("messages %q, want %d", got, len(want))
	}
	for i, w := range want {
		if !strings.Contains(got[i], w) {
			t.Errorf("message %d = %q, want it to contain %q", i, got[i], w)
		}
	}
	if msgs[3].Notice != model.NoticeTimer {
		t.Errorf("timer notice = %d", msgs[3].Notice)
	}
	// They don't count as unread.
	if c, _ := b.store.chat(b.ctx, g.String()); c.Unread != 0 {
		t.Errorf("unread = %d", c.Unread)
	}
}

// TestHistoryStub checks that history sync's stubs become system
// messages, and that ones the chat doesn't show are left out.
func TestHistoryStub(t *testing.T) {
	b := testBackend(t)
	g := types.NewJID("123", types.GroupServer)
	stub := func(id string, typ waWeb.WebMessageInfo_StubType, params ...string) *waWeb.WebMessageInfo {
		return &waWeb.WebMessageInfo{Key: &waCommon.MessageKey{ID: proto.String(id), RemoteJID: proto.String(g.String())},
			Participant: proto.String("111@lid"), MessageTimestamp: proto.Uint64(1_700_000_000),
			MessageStubType: typ.Enum(), MessageStubParameters: params}
	}
	if _, ok := b.historyStub(b.ctx, g, stub("A", waWeb.WebMessageInfo_GROUP_DELETE)); ok {
		t.Error("group delete stub kept")
	}
	if _, ok := b.historyStub(b.ctx, g, stub("B", waWeb.WebMessageInfo_GROUP_PARTICIPANT_PROMOTE, "222@lid")); ok {
		t.Error("someone else made admin kept")
	}
	m, ok := b.historyStub(b.ctx, g, stub("C", waWeb.WebMessageInfo_CALL_MISSED_GROUP_VIDEO))
	if !ok {
		t.Fatal("missed call dropped")
	}
	_ = b.store.ensureChat(b.ctx, b.db, g.String(), true, "")
	if err := b.store.putMessage(b.ctx, b.db, m); err != nil {
		t.Fatal(err)
	}
	r, ok := b.store.message(b.ctx, g.String(), "C")
	if !ok {
		t.Fatal("not stored")
	}
	if got := b.resolve(b.ctx, r, true); got.Text != "Missed group video call" || got.Notice != model.NoticeMissedCall {
		t.Errorf("got %q, notice %d", got.Text, got.Notice)
	}
	// The chat list shows it as the last message.
	if c, ok := b.store.chat(b.ctx, g.String()); !ok || c.last == nil || b.resolve(b.ctx, *c.last, true).Text != "Missed group video call" {
		t.Error("chat list doesn't show it")
	}
	if _, err := b.rawMessage(g.String(), "C"); err == nil {
		t.Error("/catch shows a system message's stub")
	}
}

func TestTimerText(t *testing.T) {
	for secs, want := range map[int]string{86400: "24 hours", 604800: "7 days", 7776000: "90 days", 3600: "1 hour"} {
		if got := timerText(secs); got != want {
			t.Errorf("timerText(%d) = %q, want %q", secs, got, want)
		}
	}
	if got := listNames([]string{"A", "B", "C"}); got != "A, B and C" {
		t.Errorf("listNames = %q", got)
	}
}
