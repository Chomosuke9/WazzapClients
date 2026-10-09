package ui

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/accounts"
	"github.com/chomosuke9/wazzapclients/internal/auto"
	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/notify"
)

// bgTest is a host without a window with two demo accounts: the open
// one, and Work, which runs in the background in the mode the test
// asks for. Each account's demo backend is opened again and again, as a
// database would be, and the opens are counted.
type bgTest struct {
	t       *testing.T
	h       *host
	l       *accounts.List
	me      *mock.Backend // the first account's
	work    *mock.Backend // Work's
	workDir string        // Work's data directory

	mu      sync.Mutex // opens happen on goroutines
	opens   map[string]int
	shown   []notify.Notification
	removed []string
	windows int // windows the host opened
}

const workDir = "accounts/2"

func newBgTest(t *testing.T, mode accounts.Mode) *bgTest {
	t.Helper()
	for p, v := range map[*time.Duration]time.Duration{&bgStartDelay: 0, &checkGrace: 0} {
		old := *p
		*p = v
		t.Cleanup(func() { *p = old })
	}
	bt := &bgTest{t: t, opens: map[string]int{}}
	show, remove := showNote, removeNote
	showNote = func(n notify.Notification) { bt.shown = append(bt.shown, n) }
	removeNote = func(id string) { bt.removed = append(bt.removed, id) }
	t.Cleanup(func() { showNote, removeNote = show, remove })

	root := t.TempDir()
	l := accounts.Load(root)
	l.Accounts[0] = accounts.Account{ID: "15550100@s.whatsapp.net", Name: "Me Myself"}
	l.Accounts = append(l.Accounts, accounts.Account{Dir: workDir, ID: "15550142@s.whatsapp.net", Name: "Work",
		Background: mode})
	bt.l, bt.workDir = l, l.Path(workDir)
	bt.me = mock.New()
	bt.work = mock.NewAccount("Work", "+1 555 0142", "15550142@s.whatsapp.net")
	open := func(dir string) (model.Backend, error) {
		bt.mu.Lock()
		defer bt.mu.Unlock()
		bt.opens[dir]++
		if dir == bt.workDir {
			return bt.work, nil
		}
		return bt.me, nil
	}
	b, _ := open(root)
	h := newHost(b, Options{Accounts: l, Open: open})
	h.notifyOK, h.notes.enabled = true, true
	h.openWin = func() { bt.windows++ }
	h.b.Start(h.poke)
	h.poll(false)
	h.startBackground()
	bt.h = h
	return bt
}

// tick runs what the background timer would, and settles.
func (bt *bgTest) tick() {
	bt.h.bgTick()
	bt.h.settle()
}

// workOpens counts the opens of Work's backend.
func (bt *bgTest) workOpens() int {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	return bt.opens[bt.workDir]
}

// flush shows every account's pending notifications and returns them.
func (bt *bgTest) flush() []notify.Notification {
	for _, n := range bt.h.notifiers() {
		n.flush()
	}
	s := bt.shown
	bt.shown = nil
	return s
}

// workRow is Work's row in the switcher.
func (bt *bgTest) workRow() accountRow {
	for _, r := range bt.h.accountRows() {
		if r.Dir == workDir {
			return r
		}
	}
	bt.t.Fatal("Work isn't in the switcher")
	return accountRow{}
}

func TestBackgroundNotifies(t *testing.T) {
	bt := newBgTest(t, accounts.Always)
	h := bt.h
	bt.tick()
	if bt.workOpens() != 1 || bt.work.Starts() != 1 || !bt.work.Background() {
		t.Fatalf("Work: %d opens, %d starts, background %v; want it started once, in the background",
			bt.workOpens(), bt.work.Starts(), bt.work.Background())
	}
	if s := bt.workRow().status(testNow()); s != "Connected in the background" {
		t.Errorf("status %q", s)
	}

	// A message for Work notifies, named after the account, even while
	// the window shows the open account (with focus).
	h.focused = true
	bt.work.Receive("rina", "Rina", "Are you coming?")
	h.settle()
	got := bt.flush()
	if len(got) != 1 {
		t.Fatalf("got %d notifications, want 1: %+v", len(got), got)
	}
	n := got[0]
	if n.ID != workDir+"|rina" || n.Title != "Rina Kartika · Work" || n.Body != "Are you coming?" {
		t.Errorf("notification %+v", n)
	}
	// The open account's own messages don't notify while it has focus.
	bt.me.Receive("rina", "Rina", "hello")
	h.settle()
	if got := bt.flush(); len(got) != 0 {
		t.Errorf("the open account notified with focus: %+v", got)
	}

	// Clicking it switches to Work, which is already running, and opens
	// the chat.
	h.focused = false
	h.handle(request{kind: reqActivate, act: notify.Activation{ID: n.ID, Action: notify.Open}})
	h.settle()
	if bt.l.Active != workDir || h.b == nil || h.openChat != "rina" || bt.windows != 1 {
		t.Fatalf("after the click: active %q, open chat %q, %d windows", bt.l.Active, h.openChat, bt.windows)
	}
	if bt.workOpens() != 1 || bt.work.Starts() != 1 || bt.work.Background() {
		t.Errorf("Work: %d opens, %d starts, background %v; want the running backend, in front",
			bt.workOpens(), bt.work.Starts(), bt.work.Background())
	}
	if !bt.me.Closed() || len(h.bg) != 0 {
		t.Errorf("the first account (only while open) didn't close: closed %v, %d in the background",
			bt.me.Closed(), len(h.bg))
	}
	// Its notification is the open account's now: reading the chat takes
	// it away.
	h.markRead("rina")
	if len(bt.removed) == 0 || bt.removed[len(bt.removed)-1] != workDir+"|rina" {
		t.Errorf("removed %v", bt.removed)
	}
}

func TestBackgroundCheck(t *testing.T) {
	bt := newBgTest(t, 15)
	h := bt.h
	bt.work.QueueOffline("rina", "Rina", "Are you coming?")
	bt.tick()
	bg := h.bg[workDir]
	if bt.workOpens() != 1 || bg == nil || bg.b == nil || !bg.checking || bg.caughtUp.IsZero() {
		t.Fatalf("check didn't run: %d opens, %+v", bt.workOpens(), bg)
	}
	if s := bt.workRow().status(testNow()); s != "Checking for messages…" {
		t.Errorf("status while checking %q", s)
	}
	got := bt.flush()
	if len(got) != 1 || got[0].ID != workDir+"|rina" || got[0].Body != "Are you coming?" {
		t.Fatalf("the message kept while offline: %+v", got)
	}
	// A message scheduled before the next check makes it come sooner.
	job := testNow().Add(5 * time.Minute)
	bg.b.(*auto.Backend).Schedule(auto.Job{Chat: "rina", At: job, Text: "On my way"})

	// Caught up: the check ends and closes the backend.
	bt.tick()
	h.awaitClosed(workDir)
	if !bt.work.Closed() || bg.b != nil || bg.checking {
		t.Fatalf("still connected after the check: closed %v, %+v", bt.work.Closed(), bg)
	}
	checked := bt.l.Find(workDir).Checked
	if checked.IsZero() || !bg.next.Equal(job) {
		t.Errorf("checked %v, next %v; want the time it ended, and the scheduled message's", checked, bg.next)
	}
	if s := bt.workRow().status(testNow()); !strings.HasPrefix(s, "Checked ") || !strings.HasSuffix(s, " · every 15 min") {
		t.Errorf("status after the check %q", s)
	}
	// The notification stays until the chat is read.
	if len(bt.removed) != 0 {
		t.Errorf("removed %v", bt.removed)
	}

	// Nothing runs until the next check is due.
	bt.tick()
	if bt.workOpens() != 1 {
		t.Errorf("opened %d times before the next check", bt.workOpens())
	}
	bg.next = timeNow()
	bt.tick()
	if bt.workOpens() != 2 || !bg.checking {
		t.Errorf("the next check didn't run: %d opens", bt.workOpens())
	}
}

func TestSwitchReusesBackground(t *testing.T) {
	bt := newBgTest(t, accounts.Always)
	h := bt.h
	bt.tick()
	root := bt.l.Path("")
	h.switchAccount(workDir)
	h.settle()
	if bt.l.Active != workDir || bt.workOpens() != 1 {
		t.Fatalf("active %q, Work opened %d times; want it taken from the background", bt.l.Active, bt.workOpens())
	}

	if len(h.bg) != 0 || !bt.me.Closed() {
		t.Fatalf("the first account (only while open) didn't close: %+v", h.bg)
	}

	// Set to always connected, the first account opens in the
	// background, and switching to it takes that backend.
	h.setBackgroundMode("", accounts.Always)
	h.bgTick()
	h.settle()
	if bg := h.bg[""]; bg == nil || bg.b == nil || bt.opens[root] != 2 || !bt.me.Background() {
		t.Fatalf("the first account didn't open in the background: %+v, opened %d times", bg, bt.opens[root])
	}
	h.switchAccount("")
	h.settle()
	if bt.opens[root] != 2 || bt.l.Active != "" || bt.me.Background() {
		t.Fatalf("first account opened %d times, active %q", bt.opens[root], bt.l.Active)
	}
	bg := h.bg[workDir]
	if bg == nil || bg.b == nil || bt.work.Closed() || !bt.work.Background() {
		t.Fatalf("Work didn't stay connected in the background: %+v, closed %v", bg, bt.work.Closed())
	}
	h.switchAccount(workDir)
	h.settle()
	if bt.workOpens() != 1 || bt.opens[root] != 2 {
		t.Errorf("opens: Work %d, first %d; want each once more at most", bt.workOpens(), bt.opens[root])
	}
	if bg := h.bg[""]; bg == nil || bg.b == nil || !bt.me.Background() {
		t.Errorf("the first account didn't go to the background: %+v", bg)
	}

	// An account checked every few minutes opens once when switched to,
	// even while a check opens it.
	h.setBackgroundMode("", 30)
	if bg := h.bg[""]; bg == nil || bg.b != nil {
		t.Fatalf("the first account still runs after changing to checks: %+v", bg)
	}
	h.bg[""].next = timeNow()
	h.bgTick() // opens it on a goroutine
	h.switchAccount("")
	h.settle()
	if bt.opens[root] != 3 || bt.l.Active != "" || len(h.bg) != 1 {
		t.Errorf("first account opened %d times (want 3), active %q, %d in the background",
			bt.opens[root], bt.l.Active, len(h.bg))
	}
}

func TestBackgroundReplyWaitsForConnection(t *testing.T) {
	bt := newBgTest(t, 60)
	h := bt.h
	bt.work.QueueOffline("rina", "Rina", "Are you coming?")
	bt.tick()
	n := bt.flush()[0]
	bt.tick() // the check ends
	h.awaitClosed(workDir)

	// Replying opens Work, and the reply goes once it's online.
	h.handle(request{kind: reqActivate, act: notify.Activation{ID: n.ID, Action: notify.Reply, Text: "Yes!"}})
	if bt.l.Active != workDir || len(h.later) != 1 {
		t.Fatalf("active %q, %d waiting; want Work, and the reply waiting", bt.l.Active, len(h.later))
	}
	h.settle()
	msgs := bt.work.Messages("rina", 1)
	if len(h.later) != 0 || len(msgs) != 1 || !msgs[0].FromMe || msgs[0].Text != "Yes!" {
		t.Errorf("reply not sent: %d waiting, last message %+v", len(h.later), msgs)
	}
	if bt.windows != 0 {
		t.Errorf("a reply opened a window")
	}
}

func TestBackgroundLogout(t *testing.T) {
	bt := newBgTest(t, accounts.Always)
	h := bt.h
	bt.tick()
	bt.work.Receive("rina", "Rina", "Are you coming?")
	h.settle()
	bt.flush()

	h.logoutAccount(workDir)
	h.settle()
	if bt.l.Find(workDir) != nil || len(h.bg) != 0 || !bt.work.Closed() {
		t.Fatalf("Work still there after logging out: %+v, %d in the background, closed %v",
			bt.l.Find(workDir), len(h.bg), bt.work.Closed())
	}
	if len(bt.removed) != 1 || bt.removed[0] != workDir+"|rina" {
		t.Errorf("its notification wasn't taken away: removed %v", bt.removed)
	}
	if rows := h.accountRows(); len(rows) != 1 {
		t.Errorf("switcher rows %+v", rows)
	}
}

func TestBackgroundModeOff(t *testing.T) {
	bt := newBgTest(t, accounts.Always)
	h := bt.h
	bt.tick()
	h.setBackgroundMode(workDir, accounts.Off)
	h.awaitClosed(workDir)
	if len(h.bg) != 0 || !bt.work.Closed() {
		t.Errorf("Work still runs after turning it off: %d in the background, closed %v", len(h.bg), bt.work.Closed())
	}
	if s := bt.workRow().status(testNow()); s != "" {
		t.Errorf("status %q", s)
	}
}

func TestSwitchAwayChecksLater(t *testing.T) {
	bt := newBgTest(t, accounts.Off)
	h := bt.h
	h.setBackgroundMode("", 15) // the open account: it applies once another opens
	if len(h.bg) != 0 {
		t.Fatalf("the open account runs in the background: %+v", h.bg)
	}
	job := testNow().Add(10 * time.Minute)
	h.b.(*auto.Backend).Schedule(auto.Job{Chat: "rina", At: job, Text: "Lunch?"})

	h.switchAccount(workDir)
	h.settle()
	bg := h.bg[""]
	if bg == nil || bg.b != nil || !bt.me.Closed() {
		t.Fatalf("the first account isn't closed and waiting to check: %+v", bg)
	}
	if !bg.next.Equal(job) || bt.l.Find("").Checked.IsZero() {
		t.Errorf("next check %v, checked %v; want the scheduled message's time, and now", bg.next, bt.l.Find("").Checked)
	}
	bt.tick()
	if bt.opens[bt.l.Path("")] != 1 {
		t.Errorf("checked right after it was open")
	}
}
