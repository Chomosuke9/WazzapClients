package ui

import (
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/notify"
)

type notifyTest struct {
	n       *notifier
	b       *mock.Backend
	shown   []notify.Notification
	removed []string
	focused bool
}

func newNotifyTest(t *testing.T) *notifyTest {
	t.Helper()
	nt := &notifyTest{b: mock.New()}
	n := newNotifier(nt.b, &host{})
	n.enabled = true
	n.show = func(x notify.Notification) { nt.shown = append(nt.shown, x) }
	n.remove = func(id string) { nt.removed = append(nt.removed, id) }
	n.tooltip = func(string) {}
	n.focused = func() bool { return nt.focused }
	n.setChats([]*model.Chat{
		{ID: "ana@lid", Name: "Ana"},
		{ID: "team@g.us", Name: "Team", IsGroup: true},
		{ID: "quiet@g.us", Name: "Quiet", IsGroup: true, Muted: true},
		{ID: "old@lid", Name: "Old", Archived: true},
	})
	n.event(model.ConnEvent{State: model.StateOnline, MeID: "me@s.whatsapp.net"})
	nt.n = n
	return nt
}

func (nt *notifyTest) receive(chat, sender, text string) *model.Message {
	m := &model.Message{ID: text, ChatID: chat, Sender: sender, Text: text, Time: testNow()}
	nt.n.event(model.MessageEvent{Msg: m, New: true})
	return m
}

func (nt *notifyTest) flush(t *testing.T) []notify.Notification {
	t.Helper()
	if nt.n.due == nil && len(nt.n.pending) > 0 {
		t.Fatal("pending messages without a flush scheduled")
	}
	nt.n.flush()
	s := nt.shown
	nt.shown = nil
	return s
}

func TestNotifyOnePerChat(t *testing.T) {
	nt := newNotifyTest(t)
	nt.receive("ana@lid", "", "hi")
	nt.receive("team@g.us", "Bob", "standup?")
	nt.receive("ana@lid", "", "are you there?")
	got := nt.flush(t)
	if len(got) != 2 {
		t.Fatalf("got %d notifications, want 2: %+v", len(got), got)
	}
	ana, team := got[0], got[1]
	if ana.ID != "ana@lid" || ana.Title != "Ana" || ana.Body != "are you there?" || ana.Footer != "2 new messages" || !ana.Reply {
		t.Errorf("one-to-one: %+v", ana)
	}
	if team.Title != "Team" || team.Body != "Bob: standup?" || team.Footer != "" {
		t.Errorf("group: %+v", team)
	}
	if ana.Image == nil {
		t.Error("no default picture")
	}
	// A later message replaces the chat's notification and counts on.
	nt.receive("ana@lid", "", "hello?")
	if got := nt.flush(t); len(got) != 1 || got[0].Footer != "3 new messages" {
		t.Errorf("third message: %+v", got)
	}
}

func TestNotifyQuietChats(t *testing.T) {
	nt := newNotifyTest(t)
	nt.receive("quiet@g.us", "Bob", "lunch")
	nt.receive("old@lid", "", "remember me?")
	nt.receive("news@newsletter", "", "headline")
	if got := nt.flush(t); len(got) != 0 {
		t.Fatalf("muted, archived and channel messages notified: %+v", got)
	}
	// Muted and archived chats still notify mentions and replies to you.
	nt.receive("quiet@g.us", "Bob", "hey ⁨"+string(model.MentionNotifies)+"@You⁩")
	m := &model.Message{ID: "r", ChatID: "old@lid", Text: "yes", Time: testNow(),
		Quote: &model.Quote{Sender: "You", Text: "ok?"}}
	nt.n.event(model.MessageEvent{Msg: m, New: true})
	got := nt.flush(t)
	if len(got) != 2 || got[0].Body != "Bob: hey @You" {
		t.Fatalf("mention and reply: %+v", got)
	}
}

func TestNotifySkips(t *testing.T) {
	nt := newNotifyTest(t)
	nt.n.event(model.MessageEvent{Msg: &model.Message{ID: "x", ChatID: "ana@lid", Text: "old"}})
	nt.n.event(model.MessageEvent{Msg: &model.Message{ID: "y", ChatID: "ana@lid", Text: "mine", FromMe: true}, New: true})
	if got := nt.flush(t); len(got) != 0 {
		t.Fatalf("history or own messages notified: %+v", got)
	}
	nt.focused = true
	nt.receive("ana@lid", "", "while focused")
	if got := nt.flush(t); len(got) != 0 {
		t.Fatalf("notified while the window has focus: %+v", got)
	}
	nt.focused = false
	setPref(nt.b, prefNotifyGroups, false)
	nt.receive("team@g.us", "Bob", "x")
	if got := nt.flush(t); len(got) != 0 {
		t.Fatalf("group notified with group notifications off: %+v", got)
	}
}

func TestNotifyPrefs(t *testing.T) {
	nt := newNotifyTest(t)
	setPref(nt.b, prefNotifyPreviews, false)
	setPref(nt.b, prefNotifySound, false)
	nt.receive("ana@lid", "", "secret")
	nt.receive("ana@lid", "", "more secret")
	got := nt.flush(t)
	if len(got) != 1 || got[0].Body != "2 new messages" || got[0].Footer != "" || !got[0].Silent {
		t.Fatalf("without previews or sound: %+v", got)
	}
}

func TestNotifyRemovedWhenRead(t *testing.T) {
	nt := newNotifyTest(t)
	nt.receive("ana@lid", "", "hi")
	nt.flush(t)
	nt.n.event(model.ChatEvent{Chat: &model.Chat{ID: "ana@lid", Name: "Ana", Unread: 1}})
	// Read on the phone.
	nt.n.event(model.ChatEvent{Chat: &model.Chat{ID: "ana@lid", Name: "Ana"}})
	if len(nt.removed) != 1 || nt.removed[0] != "ana@lid" {
		t.Fatalf("removed %v", nt.removed)
	}
	// Counting starts again.
	nt.receive("ana@lid", "", "again")
	if got := nt.flush(t); len(got) != 1 || got[0].Footer != "" {
		t.Fatalf("after reading: %+v", got)
	}
	nt.n.read("ana@lid")
	if len(nt.removed) != 2 {
		t.Fatalf("read here: removed %v", nt.removed)
	}
}

func TestNotificationText(t *testing.T) {
	for _, c := range []struct {
		m    model.Message
		want string
	}{
		{model.Message{Text: "*bold* and _it_"}, "bold and it"},
		{model.Message{Media: model.MediaImage}, "📷 Photo"},
		{model.Message{Media: model.MediaImage, Text: "beach"}, "📷 beach"},
		{model.Message{Media: model.MediaVoice, Duration: 75}, "🎤 Voice message (1:15)"},
		{model.Message{Media: model.MediaDocument, Text: "report.pdf"}, "📄 report.pdf"},
	} {
		if got := notificationText(&c.m); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.m, got, c.want)
		}
	}
}
