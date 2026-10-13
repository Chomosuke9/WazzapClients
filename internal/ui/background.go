package ui

import (
	"log"
	"strconv"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/accounts"
	"github.com/chomosuke9/wazzapclients/internal/auto"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// Accounts that aren't open can run in the background, as their
// accounts.Mode says: connected all the time (Always), or checked every
// few minutes, which connects long enough to take the messages WhatsApp
// kept (until a CaughtUpEvent) and closes again. Each has a backend of
// its own, wrapped with auto like the open one's, so scheduled messages
// and AFK replies go on, and a notifier, which outlives the backend
// between checks with the notifications it shows. The host polls them on
// the UI goroutine like the open backend, but their events reach no
// window.
//
// Their backends open and close on goroutines of their own (an open
// migrates the database and lists the chats) and come back through
// bgOpen. Switching to one takes its backend as it is (switchAccount),
// and the account switched away from goes on in the background if its
// mode says so (closeAccount).

// Timings of background accounts; tests shorten them.
var (
	// bgStartDelay is how long after the app starts its background
	// accounts open or check, so the open account comes up first.
	bgStartDelay = 5 * time.Second
	// checkGrace is how long a check stays connected once it caught up:
	// the last messages notify, and AFK replies and scheduled messages go.
	checkGrace = 3 * time.Second
	// checkTimeout ends a check that never catches up (no network).
	checkTimeout = 90 * time.Second
	// bgRetry is how long an account that is always connected waits to
	// open again after it failed.
	bgRetry = 5 * time.Minute
	// logoutTimeout gives up logging out an account that never connects.
	logoutTimeout = time.Minute
	// bgMaxWait wakes the background timer at least this often, in case
	// the clock jumped (the computer slept).
	bgMaxWait = 10 * time.Minute
)

// bgAccount is an account in the background.
type bgAccount struct {
	dir string
	// b is its backend, wrapped with auto, or nil while it's closed
	// (between checks, or while opening).
	b       model.Backend
	notes   *notifier
	conn    model.ConnEvent
	opening bool // openBg runs
	// A check: it closes checkGrace after it caught up, once nothing it
	// sent is still on its way, or at checkTimeout.
	checking bool
	started  time.Time // when it opened (and a logout's start)
	caughtUp time.Time
	sending  map[string]bool // its messages still on their way
	// next is when it opens again: the next check, or a retry.
	next time.Time
	// nextJob is when its next scheduled message is due, as of when it
	// last closed; it checks then too, since auto drops late messages.
	nextJob time.Time
	failed  bool // the last check or connection failed
	// loggingOut is set by logoutAccount: it logs out once it's online,
	// and leaves the list once logged out.
	loggingOut bool
	askedOut   bool // Logout was called
}

// bgOpened is a background account's backend, opened by openBg.
type bgOpened struct {
	dir   string
	b     model.Backend
	chats []*model.Chat
	err   error
}

// bgMode is how the account with dir runs now: Off when it's the open
// one, gone, or not linked.
func (h *host) bgMode(dir string) accounts.Mode {
	l := h.o.Accounts
	if l == nil || h.o.Open == nil || dir == l.Active {
		return accounts.Off
	}
	if a := l.Find(dir); a != nil && a.InBackground() {
		return a.Background
	}
	return accounts.Off
}

// accountLabel names the account with dir in notifications and toasts.
func (h *host) accountLabel(dir string) string {
	if l := h.o.Accounts; l != nil {
		if a := l.Find(dir); a != nil && a.Label() != "" {
			return a.Label()
		}
	}
	return "Another account"
}

// startBackground starts the accounts that run in the background as the
// app starts, a moment after the open one. Those checked every few
// minutes check then too, which also learns when their scheduled
// messages are due.
func (h *host) startBackground() {
	l := h.o.Accounts
	if l == nil {
		return
	}
	at := timeNow().Add(bgStartDelay)
	for _, a := range l.Accounts {
		if h.bgMode(a.Dir) != accounts.Off && h.bg[a.Dir] == nil {
			h.bg[a.Dir] = &bgAccount{dir: a.Dir, next: at}
		}
	}
	h.armBg()
}

// setBackgroundMode sets how the account with dir runs while another one
// is open, and starts, stops or reschedules it to match.
func (h *host) setBackgroundMode(dir string, m accounts.Mode) {
	l := h.o.Accounts
	if l == nil {
		return
	}
	a := l.Find(dir)
	if a == nil || a.Background == m {
		return
	}
	a.Background = m
	h.saveAccounts()
	h.applyMode(dir)
	h.refreshAccounts()
}

// applyMode makes the background account with dir run as its mode says.
func (h *host) applyMode(dir string) {
	mode := h.bgMode(dir)
	bg := h.bg[dir]
	now := timeNow()
	switch {
	case bg != nil && bg.loggingOut:
		return // it leaves the list
	case mode == accounts.Off:
		h.stopBg(dir)
		return
	case bg == nil:
		bg = &bgAccount{dir: dir}
		h.bg[dir] = bg
	}
	switch {
	case mode == accounts.Always:
		bg.checking = false // a check that runs stays connected
		if bg.b == nil && !bg.opening {
			bg.next = now
		}
	case bg.b != nil && !bg.checking:
		// It was connected all along: caught up already.
		h.closeBg(bg)
		h.scheduleCheck(bg, now, mode)
	case bg.b == nil && !bg.opening:
		bg.next = now // check now, to show it works
	}
	h.armBg()
}

// bgTick does what is due: checks start and end, accounts that failed
// open again, and logouts that never connected give up.
func (h *host) bgTick() {
	now := timeNow()
	for dir, bg := range h.bg {
		mode := h.bgMode(dir)
		switch {
		case bg.loggingOut:
			if now.Sub(bg.started) >= logoutTimeout {
				h.toast("Couldn't log out of " + h.accountLabel(dir) + ". Check your connection and try again.")
				bg.loggingOut, bg.askedOut = false, false
				if mode == accounts.Off {
					h.stopBg(dir)
				} else {
					h.closeBg(bg)
					h.scheduleCheck(bg, now, mode)
				}
				h.refreshAccounts()
			}
		case mode == accounts.Off:
			h.stopBg(dir)
		case bg.checking && bg.b != nil:
			done := !bg.caughtUp.IsZero() && now.Sub(bg.caughtUp) >= checkGrace && len(bg.sending) == 0
			if done || now.Sub(bg.started) >= checkTimeout {
				h.endCheck(bg, done)
			}
		case bg.b == nil && !bg.opening && !now.Before(bg.next):
			h.openBg(bg, mode != accounts.Always)
		}
	}
	h.armBg()
}

// armBg sets the timer for the next thing bgTick does.
func (h *host) armBg() {
	var at time.Time
	next := func(t time.Time) {
		if at.IsZero() || t.Before(at) {
			at = t
		}
	}
	for _, bg := range h.bg {
		switch {
		case bg.loggingOut:
			next(bg.started.Add(logoutTimeout))
		case bg.checking && bg.b != nil:
			next(bg.started.Add(checkTimeout))
			if !bg.caughtUp.IsZero() {
				next(bg.caughtUp.Add(checkGrace))
			}
		case bg.b == nil && !bg.opening:
			next(bg.next)
		}
	}
	if h.bgTimer != nil {
		h.bgTimer.Stop()
	}
	if at.IsZero() {
		return
	}
	due := h.bgDue
	h.bgTimer = time.AfterFunc(min(max(at.Sub(timeNow()), 0), bgMaxWait), func() {
		select {
		case due <- struct{}{}:
		default:
		}
	})
}

// openBg opens a background account's backend on a goroutine, after the
// last one of the account has closed; bgOpened takes it from there.
func (h *host) openBg(bg *bgAccount, check bool) {
	bg.opening, bg.checking = true, check
	bg.started = timeNow()
	dir, path, open, out := bg.dir, h.o.Accounts.Path(bg.dir), h.o.Open, h.bgOpen
	closing := h.closing[dir]
	delete(h.closing, dir)
	go func() {
		if closing != nil {
			<-closing
		}
		b, err := open(path)
		var chats []*model.Chat
		if err == nil {
			chats = b.Chats()
		}
		out <- bgOpened{dir: dir, b: b, chats: chats, err: err}
	}()
}

// bgOpened takes a backend openBg opened.
func (h *host) bgOpened(o bgOpened) {
	bg := h.bg[o.dir]
	if bg == nil || !bg.opening {
		// Stopped meanwhile.
		if o.err == nil {
			h.closeLater(o.dir, o.b)
		}
		return
	}
	bg.opening = false
	if o.err != nil {
		log.Printf("open account %s: %v", o.dir, o.err)
		bg.failed, bg.checking = true, false
		bg.next = timeNow().Add(bgRetry)
		if every := h.bgMode(o.dir).Every(); every > 0 {
			bg.next = timeNow().Add(every)
		}
		h.armBg()
		h.refreshAccounts()
		return
	}
	h.startBg(bg, o.b, o.chats)
}

// startBg starts the backend of a background account, which chats are
// the chats of.
func (h *host) startBg(bg *bgAccount, b model.Backend, chats []*model.Chat) {
	b, _ = withAuto(b)
	if x, ok := b.(model.Backgrounder); ok {
		x.SetBackground(true)
	}
	if bg.notes == nil {
		bg.notes = h.newNotifier(b, bg.dir)
	}
	bg.notes.b = b
	h.backgroundNotes(bg.notes, bg.dir)
	bg.notes.setChats(chats)
	bg.b, bg.conn = b, model.ConnEvent{State: model.StateConnecting}
	bg.caughtUp, bg.sending = time.Time{}, map[string]bool{}
	b.Start(h.poke)
	h.armBg()
	h.refreshAccounts()
}

// endCheck closes a check's backend and sets when the next one runs. ok
// is false for one that never caught up.
func (h *host) endCheck(bg *bgAccount, ok bool) {
	now := timeNow()
	bg.checking, bg.failed = false, !ok
	if a := h.o.Accounts.Find(bg.dir); a != nil && ok {
		a.Checked = now
		h.saveAccounts()
	}
	h.closeBg(bg)
	h.scheduleCheck(bg, now, h.bgMode(bg.dir))
	h.refreshAccounts()
}

// scheduleCheck sets when bg, in mode m, checks next: once the interval
// has passed, or when its next scheduled message is due, if that's
// sooner.
func (h *host) scheduleCheck(bg *bgAccount, now time.Time, m accounts.Mode) {
	bg.next = now.Add(m.Every())
	if j := bg.nextJob; j.After(now) && j.Before(bg.next) {
		bg.next = j
	}
	h.armBg()
}

// closeBg closes a background account's backend, on a goroutine. Its
// notifier stays, with the notifications it shows.
func (h *host) closeBg(bg *bgAccount) {
	b := bg.b
	if b == nil {
		return
	}
	if a, ok := b.(*auto.Backend); ok {
		bg.nextJob = a.Next()
	}
	if bg.notes != nil && len(bg.notes.pending) > 0 {
		bg.notes.flush()
		h.armNotes()
	}
	bg.b, bg.conn, bg.caughtUp, bg.sending = nil, model.ConnEvent{}, time.Time{}, nil
	h.closeLater(bg.dir, b)
}

// closeLater closes b, the backend of the account with dir, on a
// goroutine. The next open of the account waits for it.
func (h *host) closeLater(dir string, b model.Backend) {
	done := make(chan struct{})
	prev := h.closing[dir]
	h.closing[dir] = done
	go func() {
		if prev != nil {
			<-prev
		}
		b.Close()
		close(done)
	}()
}

// awaitClosed waits until the backends of the account with dir that are
// closing have closed.
func (h *host) awaitClosed(dir string) {
	if c := h.closing[dir]; c != nil {
		<-c
		delete(h.closing, dir)
	}
}

// stopBg stops the background account with dir: its backend closes and
// its notifications go.
func (h *host) stopBg(dir string) {
	bg := h.bg[dir]
	if bg == nil {
		return
	}
	delete(h.bg, dir)
	if bg.notes != nil {
		bg.notes.clear()
	}
	h.closeBg(bg)
	h.updateTooltip()
	h.armNotes()
}

// pollBackground drains the events of the background accounts'
// backends.
func (h *host) pollBackground() {
	for _, bg := range h.bg {
		if bg.b == nil {
			continue
		}
		if evs := bg.b.Poll(); len(evs) > 0 {
			h.bgEvents(bg, evs)
			h.quietTrim()
		}
	}
}

// bgEvents takes in the events of a background account's backend.
func (h *host) bgEvents(bg *bgAccount, evs []model.Event) {
	state := bg.conn.State
	var out *model.ConnEvent // logged out, or failed
	for _, ev := range evs {
		switch e := ev.(type) {
		case model.ConnEvent:
			if e.Me == "" {
				e.Me = bg.conn.Me
			}
			if e.MeID == "" {
				e.MeID = bg.conn.MeID
			}
			bg.conn = e
			switch e.State {
			case model.StateStarting, model.StateQR, model.StateQRExpired, model.StateError:
				out = &e
			}
		case model.CaughtUpEvent:
			bg.caughtUp = timeNow()
		case model.MessageEvent:
			if m := e.Msg; m != nil && m.FromMe {
				if m.Receipt == model.Pending {
					bg.sending[m.ID] = true
				} else {
					delete(bg.sending, m.ID)
				}
			}
		case model.ReceiptEvent:
			for _, id := range e.IDs {
				delete(bg.sending, id)
			}
		case model.NoticeEvent:
			h.toast(h.accountLabel(bg.dir) + ": " + e.Text)
		}
		bg.notes.event(ev)
	}
	switch {
	case out != nil && out.State == model.StateError:
		log.Printf("account %s in the background: %s", bg.dir, out.Err)
		bg.failed = true
		if bg.checking {
			h.endCheck(bg, false)
		} else {
			h.closeBg(bg)
			bg.next = timeNow().Add(bgRetry)
		}
	case out != nil:
		h.bgLoggedOut(bg)
		return
	case bg.conn.State == model.StateOnline:
		bg.failed = false
		if bg.loggingOut && !bg.askedOut {
			bg.askedOut = true
			bg.b.Logout()
		}
	}
	if bg.conn.State != state {
		h.refreshAccounts()
	}
	h.armBg()
}

// bgLoggedOut takes a background account that was logged out (on the
// phone, or from Settings) off the list, as the open account's logout
// does: its backend wiped its messages and session already.
func (h *host) bgLoggedOut(bg *bgAccount) {
	label := h.accountLabel(bg.dir)
	delete(h.bg, bg.dir)
	if bg.notes != nil {
		bg.notes.clear()
	}
	if bg.b != nil {
		// Here, not on a goroutine: its directory goes next.
		bg.b.Close()
		bg.b = nil
	}
	h.awaitClosed(bg.dir)
	if err := h.o.Accounts.Remove(bg.dir); err != nil {
		log.Printf("remove account: %v", err)
	}
	h.saveAccounts()
	h.updateTooltip()
	h.armNotes()
	h.refreshAccounts()
	if bg.loggingOut {
		h.toast("Logged out of " + label)
	} else {
		h.toast(label + " was logged out")
	}
}

// logoutAccount logs out the account with dir, which isn't the open one:
// it connects first if it has to, and leaves the list once logged out
// (bgLoggedOut).
func (h *host) logoutAccount(dir string) {
	l := h.o.Accounts
	if l == nil || h.o.Open == nil {
		return
	}
	if dir == l.Active {
		h.logout()
		return
	}
	if a := l.Find(dir); a == nil || !a.Linked() {
		return
	}
	bg := h.bg[dir]
	if bg == nil {
		bg = &bgAccount{dir: dir}
		h.bg[dir] = bg
	}
	if bg.loggingOut {
		return
	}
	bg.loggingOut, bg.askedOut, bg.checking = true, false, false
	bg.started = timeNow()
	switch {
	case bg.b != nil && bg.conn.State == model.StateOnline:
		bg.askedOut = true
		bg.b.Logout()
	case bg.b == nil && !bg.opening:
		h.openBg(bg, false)
		bg.started = timeNow()
	}
	h.armBg()
	h.refreshAccounts()
}

// newNotifier makes the notifier of the account with dir.
func (h *host) newNotifier(b model.Backend, dir string) *notifier {
	n := newNotifier(b, h)
	n.enabled = h.notifyOK
	if h.o.Accounts != nil {
		n.prefix = dir + "|"
	}
	n.arm = h.armNotes
	n.tooltip = func(string) { h.updateTooltip() }
	return n
}

// openNotes makes n the open account's: nothing shows while the window
// has focus, and titles don't name the account.
func (h *host) openNotes(n *notifier) {
	n.account, n.app = "", nil
	n.focused = func() bool { return h.focused }
}

// backgroundNotes makes n a background account's: titles name the
// account, the app's preferences come from the open account, and
// notifications show while the window has focus, since it doesn't show
// the account's chats.
func (h *host) backgroundNotes(n *notifier, dir string) {
	n.account = h.accountLabel(dir)
	n.app = func() model.Backend { return h.b }
	n.focused = func() bool { return false }
}

// notifiers lists the notifiers of all the accounts, the open one's
// first.
func (h *host) notifiers() []*notifier {
	ns := make([]*notifier, 0, 1+len(h.bg))
	if h.notes != nil {
		ns = append(ns, h.notes)
	}
	for _, bg := range h.bg {
		if bg.notes != nil {
			ns = append(ns, bg.notes)
		}
	}
	return ns
}

// armNotes sets the timer for the notifiers' next flush.
func (h *host) armNotes() {
	var at time.Time
	for _, n := range h.notifiers() {
		if !n.flushAt.IsZero() && (at.IsZero() || n.flushAt.Before(at)) {
			at = n.flushAt
		}
	}
	if h.notesTimer != nil {
		h.notesTimer.Stop()
	}
	if at.IsZero() {
		return
	}
	due := h.notesDue
	h.notesTimer = time.AfterFunc(max(time.Until(at), 0), func() {
		select {
		case due <- struct{}{}:
		default:
		}
	})
}

// flushNotes shows the notifications that are due.
func (h *host) flushNotes() {
	for _, n := range h.notifiers() {
		if !n.flushAt.IsZero() && !n.now().Before(n.flushAt) {
			n.flush()
		}
	}
	h.armNotes()
}

// updateTooltip counts every account's unread chats on the tray icon.
func (h *host) updateTooltip() {
	total := 0
	for _, n := range h.notifiers() {
		total += max(n.unread, 0)
	}
	setTooltip(tooltipText(total))
}

// bgView is what a background account does, for its status line.
type bgView struct {
	running    bool // its backend is open
	checking   bool
	state      model.ConnState
	failed     bool
	loggingOut bool
}

// bgView describes the background account with dir.
func (h *host) bgView(dir string) bgView {
	bg := h.bg[dir]
	if bg == nil {
		return bgView{}
	}
	return bgView{running: bg.b != nil, checking: bg.checking && (bg.b != nil || bg.opening),
		state: bg.conn.State, failed: bg.failed, loggingOut: bg.loggingOut}
}

// status describes how an account that isn't open runs: its mode and
// what it's doing, or when it last checked. It's "" for the open account
// and for one that runs only while open.
func (r accountRow) status(now time.Time) string {
	if r.bg.loggingOut {
		return "Logging out…"
	}
	if r.active || !r.InBackground() {
		return ""
	}
	if r.Background == accounts.Always {
		switch {
		case r.bg.running && r.bg.state == model.StateOnline:
			return "Connected in the background"
		case r.bg.failed:
			return "Couldn't connect, trying again soon"
		case r.bg.running && r.bg.state == model.StateOffline:
			return "Reconnecting in the background…"
		}
		return "Connecting in the background…"
	}
	every := "every " + strconv.Itoa(int(r.Background)) + " min"
	if r.Background == 60 {
		every = "every hour"
	}
	switch {
	case r.bg.checking:
		return "Checking for messages…"
	case r.bg.failed:
		return "Last check failed · " + every
	case r.Checked.IsZero():
		return "Checks " + every
	}
	at := r.Checked.In(now.Location())
	when := at.Format("15:04")
	if y, m, d := at.Date(); y != now.Year() || m != now.Month() || d != now.Day() {
		when = at.Format("2 Jan")
	}
	return "Checked " + when + " · " + every
}
