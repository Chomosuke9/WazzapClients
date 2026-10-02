package auto

import (
	"strings"
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

var base = time.Date(2026, time.October, 2, 15, 0, 0, 0, time.UTC)

// fake is the demo backend, with messages coming in when a test says.
type fake struct {
	*mock.Backend
	in []model.Event
}

func (f *fake) Poll() []model.Event {
	evs := append(f.Backend.Poll(), f.in...)
	f.in = nil
	return evs
}

type autoTest struct {
	t   *testing.T
	f   *fake
	a   *Backend
	now time.Time
}

func newAutoTest(t *testing.T) *autoTest {
	mock.Clock = func() time.Time { return base }
	at := &autoTest{t: t, f: &fake{Backend: mock.New()}, now: base}
	at.a = Wrap(at.f, func() time.Time { return at.now })
	at.a.Start(func() {})
	at.a.Poll() // online
	return at
}

// sent returns the messages Poll says it sent.
func (at *autoTest) poll() (msgs []*model.Message, notices []string) {
	for _, ev := range at.a.Poll() {
		switch e := ev.(type) {
		case model.MessageEvent:
			if e.Msg.FromMe {
				msgs = append(msgs, e.Msg)
			}
		case model.NoticeEvent:
			notices = append(notices, e.Text)
		}
	}
	return msgs, notices
}

func TestSchedule(t *testing.T) {
	at := newAutoTest(t)
	j := at.a.Schedule(Job{Chat: "rina", At: base.Add(time.Hour), Text: "later", Reply: "rina-2"})
	at.a.Schedule(Job{Chat: "work", At: base.Add(30 * time.Minute), Text: "sooner"})
	if js := at.a.Jobs(""); len(js) != 2 || js[0].Text != "sooner" || len(at.a.Jobs("rina")) != 1 {
		t.Fatalf("jobs %+v", js)
	}
	// They're kept in the prefs.
	if again := Wrap(at.f, func() time.Time { return at.now }); len(again.Jobs("")) != 2 {
		t.Fatal("the jobs weren't saved")
	}
	if ms, _ := at.poll(); len(ms) != 0 {
		t.Fatalf("sent %d early", len(ms))
	}
	at.now = base.Add(32 * time.Minute)
	ms, _ := at.poll()
	if len(ms) != 1 || ms[0].Text != "sooner" || ms[0].ChatID != "work" {
		t.Fatalf("sent %+v", ms)
	}
	// Offline, nothing goes until it's back.
	at.f.in = append(at.f.in, model.ConnEvent{State: model.StateConnecting})
	at.now = base.Add(time.Hour + 3*time.Minute) // a little late is fine
	if ms, _ := at.poll(); len(ms) != 0 {
		t.Fatal("sent while offline")
	}
	at.f.in = append(at.f.in, model.ConnEvent{State: model.StateOnline})
	ms, _ = at.poll()
	if len(ms) != 1 || ms[0].Text != "later" || ms[0].Quote == nil {
		t.Fatalf("sent %+v", ms)
	}
	if at.a.Cancel(j.ID) || len(at.a.Jobs("")) != 0 || at.f.Pref(prefJobs) != "" {
		t.Fatal("the queue isn't empty")
	}
}

func TestScheduleTooLate(t *testing.T) {
	at := newAutoTest(t)
	at.a.Schedule(Job{Chat: "rina", At: base.Add(time.Hour), Text: "good morning everyone, coffee's on me today", Shown: ""})
	at.a.Schedule(Job{Chat: "rina", At: base.Add(3 * time.Hour), Text: "later"})
	// The app was closed: it opens two hours late.
	at.now = base.Add(3 * time.Hour)
	ms, ns := at.poll()
	if len(ms) != 1 || ms[0].Text != "later" {
		t.Fatalf("sent %+v", ms)
	}
	want := "Not sent, the app was closed or offline at 16:00: \"good morning everyone, coffee's on me t…\""
	if len(ns) != 1 || ns[0] != want {
		t.Fatalf("notices %q, want %q", ns, want)
	}
	if len(at.a.Jobs("")) != 0 {
		t.Fatal("the late message stayed queued")
	}
}

func TestCancelAndSendNow(t *testing.T) {
	at := newAutoTest(t)
	a := at.a.Schedule(Job{Chat: "rina", At: base.Add(time.Hour), Text: "a"})
	b := at.a.Schedule(Job{Chat: "rina", At: base.Add(time.Hour), Text: "b"})
	if !at.a.Cancel(a.ID) {
		t.Fatal("couldn't cancel")
	}
	if m := at.a.SendNow(b.ID); m == nil || m.Text != "b" {
		t.Fatalf("sent %v", m)
	}
	if at.a.SendNow(b.ID) != nil || len(at.a.Jobs("")) != 0 {
		t.Fatal("sent twice")
	}
}

// mentioned is a mention as backends resolve it, marked as notifying you.
func mentioned(name string) string {
	return "⁨" + string(model.MentionNotifies) + "@" + name + "⁩"
}

func replyToMe(id, chat, sender string, t time.Time) model.Event {
	ev := incoming(id, chat, sender, "agreed", t).(model.MessageEvent)
	ev.Msg.Quote = &model.Quote{Sender: "You", Text: "ship it?"}
	return ev
}

func incoming(id, chat, sender, text string, t time.Time) model.Event {
	return model.MessageEvent{New: true, Msg: &model.Message{ID: id, ChatID: chat, Sender: sender, SenderID: sender,
		Text: text, Time: t}}
}

func TestAway(t *testing.T) {
	at := newAutoTest(t)
	at.a.SetAway("lunch")
	at.now = base.Add(10 * time.Minute)
	at.f.in = append(at.f.in,
		incoming("x1", "rina", "rina@lid", "hey", at.now),
		incoming("x2", "rina", "rina@lid", "you there?", at.now),    // told already
		incoming("x3", "work", "budi@lid", "lunch anyone?", at.now), // a group, not to you
		incoming("x4", "work", "dewi@lid", "ping "+mentioned("You"), at.now),
		incoming("x6", "work", "andre@lid", mentioned("all")+" standup", at.now), // not @all
		replyToMe("x7", "work", "clara@lid", at.now),                             // nor a reply in a group
		incoming("x5", "rina", "rina@lid", "old", base.Add(-time.Hour)),          // from before
	)
	ms, _ := at.poll()
	if len(ms) != 2 || ms[0].ChatID != "rina" || ms[1].ChatID != "work" || ms[1].Quote == nil || ms[1].Quote.ID != "x4" {
		t.Fatalf("replied %+v", ms)
	}
	if want := "💤 *AFK*: lunch\n_Away since 15:00_"; ms[0].Text != want || ms[0].Quote == nil {
		t.Fatalf("reply %q, want %q", ms[0].Text, want)
	}
	// Its own replies coming back don't end it.
	at.f.in = append(at.f.in, model.MessageEvent{Msg: ms[0]})
	if _, ns := at.poll(); len(ns) != 0 || at.a.Away() == nil {
		t.Fatal("AFK ended by its own reply")
	}
	// It's kept in the prefs.
	if again := Wrap(at.f, func() time.Time { return at.now }); again.Away() == nil || len(again.Away().Told) != 2 {
		t.Fatal("AFK wasn't saved")
	}
	// Sending a message ends it.
	at.a.Send("rina", model.Draft{Text: "back"})
	if _, ns := at.poll(); len(ns) != 1 || !strings.Contains(ns[0], "2 people") || at.a.Away() != nil {
		t.Fatalf("notices %q", ns)
	}
	if at.f.Pref(prefAway) != "" {
		t.Fatal("AFK stayed in the prefs")
	}
}

func TestAwayEndsFromAnotherDevice(t *testing.T) {
	at := newAutoTest(t)
	at.a.SetAway("")
	at.f.in = append(at.f.in, model.MessageEvent{Msg: &model.Message{ID: "p1", ChatID: "rina", FromMe: true,
		Time: base.Add(time.Minute)}})
	if _, ns := at.poll(); len(ns) != 1 || ns[0] != "Welcome back! You're no longer AFK." {
		t.Fatalf("notices %q", ns)
	}
}

func TestAwayText(t *testing.T) {
	now := base.Add(26 * time.Hour)
	for since, want := range map[time.Time]string{
		now.Add(-time.Hour):    "💤 *AFK*\n_Away since 16:00_",
		base:                   "💤 *AFK*\n_Away since Fri 2 Oct 15:00_",
		base.AddDate(-1, 0, 0): "💤 *AFK*\n_Away since 2 Oct 2025 15:00_",
	} {
		if got := AwayText(&Away{Since: since}, now); got != want {
			t.Errorf("AwayText(%v) = %q, want %q", since, got, want)
		}
	}
}
