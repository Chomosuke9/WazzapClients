package ui

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/system"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/chomosuke9/wazzapclients/internal/accounts"
	"github.com/chomosuke9/wazzapclients/internal/auto"
	"github.com/chomosuke9/wazzapclients/internal/desktop"
	"github.com/chomosuke9/wazzapclients/internal/memtrim"
	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/notify"
	"github.com/chomosuke9/wazzapclients/internal/ui/icon"
)

const (
	appName = "WazzapClients"
	// appID identifies the app to Windows' notification center.
	appID = "WazzapClients.Desktop"
)

// Options configure Run.
type Options struct {
	// Window holds the options of every window Run opens.
	Window []app.Option
	// Hidden starts the app in the background, without a window, as when
	// it starts at login. It's ignored where the app can't run in the
	// background (no tray icon).
	Hidden bool
	// Relaunch are the arguments that start this instance again with the
	// same data, for starting at login and for notification clicks while
	// the app isn't running. nil means it can't be started again (demo
	// data).
	Relaunch []string
	// NotifyDir keeps the pictures notifications show.
	NotifyDir string
	// Accounts lists the accounts linked on this computer, and Open opens
	// the backend of one's data directory; the backend given to Run is
	// the open one's. With nil Accounts there is only that backend.
	Accounts *accounts.List
	Open     func(dir string) (model.Backend, error)
	// Version is the release this build is ("v1.2.3"), or "" for a
	// development build, which doesn't update itself.
	Version string
}

// Run runs the app until it quits. It opens a window and, while a tray
// icon is up, closing the window doesn't quit: the window is destroyed,
// which frees everything it drew with, and the connection and
// notifications go on in the background until the tray icon, a
// notification or a second launch opens a new window.
//
// The goroutine calling Run is the UI goroutine for good: it handles the
// window's events, the backend's and the requests from the tray and from
// notifications. A helper goroutine waits for each window event and hands
// it over (see openWindow), so requests are served even while a window is
// open but minimized, when Gio draws no frames.
func Run(b model.Backend, o Options) error {
	b, _ = withAuto(b)
	h := &host{
		b:       b,
		o:       o,
		wake:    make(chan struct{}, 1),
		reqs:    make(chan request, 16),
		syncPct: -1,
		drafts:  make(map[string]*chatDraft),
	}
	h.notes = newNotifier(b, h)
	h.upd.h = h
	if exe, err := os.Executable(); err == nil {
		h.upd.exe = exe
	}
	var cmd []string
	if exe, err := os.Executable(); err == nil && o.Relaunch != nil {
		cmd = append([]string{exe}, o.Relaunch...)
	}
	if err := notify.Init(notify.Options{
		AppID:    appID,
		Name:     appName,
		Icon:     encodePNG(appIcon(256)),
		Dir:      o.NotifyDir,
		Command:  cmd,
		Activate: func(a notify.Activation) { h.request(request{kind: reqActivate, act: a}) },
	}); err == nil {
		h.notifyOK = true
		h.notes.enabled = true
		defer notify.Close()
	}
	if err := desktop.TrayStart(desktop.Tray{
		Name: appName,
		Icon: icon.Tray,
		Open: func() { h.request(request{kind: reqShow}) },
		Quit: func() { h.request(request{kind: reqQuit}) },
	}); err == nil {
		h.tray = true
		defer desktop.TrayStop()
	}
	defer func() { h.b.Close() }()
	h.notes.setChats(b.Chats())
	if !o.Hidden || !h.tray {
		h.openWindow()
	}
	b.Start(h.poke)
	for {
		select {
		case e := <-h.events:
			closed, err := h.windowEvent(e)
			h.ack <- struct{}{}
			if closed {
				h.closeWindow()
				if err != nil {
					return err
				}
				if h.quitting || !h.background() {
					return nil
				}
			}
		case <-h.wake:
			h.poll(true)
		case r := <-h.reqs:
			if h.handle(r) {
				return nil
			}
		case <-h.notes.due:
			h.notes.flush()
		}
	}
}

// host is the app outside its window: the backend, the notifier, the tray
// icon, and the window while one is open.
type host struct {
	b     model.Backend
	o     Options
	notes *notifier
	// notifyOK is set once notifications work (notify.Init).
	notifyOK bool
	tray     bool          // a tray icon is up: the app can run without a window
	wake     chan struct{} // the backend queued events
	reqs     chan request

	// The open window, or nil.
	win     *app.Window
	u       *UI
	events  chan event.Event // the window's events, see openWindow
	ack     chan struct{}    // an event was handled
	ops     op.Ops
	clock   frameClock
	focused bool
	hwnd    uintptr // the window's handle on Windows, once known
	// Files dragged over the window (see desktop.EnableDrop): dragging is
	// set while a drag with files is over it, and drops gets the paths.
	dragging atomic.Bool
	drops    chan []string
	idle     *time.Timer // memory trims, see idleTrim
	away     *time.Timer
	bgTrim   *time.Timer // the trim while there is no window

	// The size the next window opens at.
	size      image.Point // in dp; zero for the default
	maximized bool
	pxPerDp   float32

	queue    []model.Event   // backend events for u
	conn     model.ConnEvent // the latest state, for new windows
	syncPct  int
	openChat string                // chat to open in the next window
	drafts   map[string]*chatDraft // the chats' drafts, kept across windows
	quitting bool

	// leaving is set while the open account logs out to switch to
	// another one (see logout): once it's logged out, the app opens
	// leaveTo.
	leaving bool
	leaveTo string
	// notice is shown in the next UI (a toast after switching accounts).
	notice string
	upd    updater
}

type request struct {
	kind reqKind
	act  notify.Activation
	dir  string // the account of reqSwitch
}

type reqKind int

const (
	reqShow       reqKind = iota // bring the window up (tray icon, second launch)
	reqQuit                      // the tray menu's Quit
	reqActivate                  // a notification or one of its buttons
	reqSwitch                    // open another account
	reqAddAccount                // link a new account
	reqRedraw                    // an update moved on: show it
	reqRestart                   // an update is in place: start it and quit
)

// request queues r for the UI goroutine. It's called from other
// goroutines and never blocks: a flood of clicks drops the extra ones.
func (h *host) request(r request) {
	select {
	case h.reqs <- r:
	default:
	}
}

// poke is the backend's notify function.
func (h *host) poke() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// background reports whether closing the window leaves the app running.
func (h *host) background() bool {
	return h.tray && h.conn.State.LoggedIn() && prefOn(h.b, prefBackground)
}

// poll drains the backend's events: the notifier sees them all, and the
// window's UI gets them on its next frame.
func (h *host) poll(invalidate bool) {
	evs := h.b.Poll()
	for _, ev := range evs {
		switch e := ev.(type) {
		case model.ConnEvent:
			me, meID := h.conn.Me, h.conn.MeID
			h.conn = e
			if e.Me == "" {
				h.conn.Me = me
			}
			if e.MeID == "" {
				h.conn.MeID = meID
			}
			if h.win == nil && (e.State == model.StateQR || e.State == model.StateError) {
				// Linking again, or a problem: only the window can show it.
				h.show()
			}
			h.accountState(e)
		case model.AccountEvent:
			h.noteAccount(nil)
		case model.SyncEvent:
			h.syncPct = e.Percent
			if e.Percent >= 100 {
				h.syncPct = -1
			}
		}
		h.notes.event(ev)
	}
	switch {
	case len(evs) == 0:
	case h.u != nil:
		h.queue = append(h.queue, evs...)
		if invalidate {
			h.win.Invalidate()
		}
	case h.bgTrim == nil:
		h.bgTrim = time.AfterFunc(idleTrim, memtrim.Trim)
	default:
		// Without a window, trim once the backend has been quiet a while.
		h.bgTrim.Reset(idleTrim)
	}
}

// uiEvents is Poll for the window's UI.
func (h *host) uiEvents() []model.Event {
	h.poll(false)
	ev := h.queue
	h.queue = nil
	return ev
}

// handle serves a request. It reports whether the app should quit now.
func (h *host) handle(r request) bool {
	switch r.kind {
	case reqShow:
		h.show()
	case reqQuit:
		if h.win == nil {
			return true
		}
		h.quitting = true
		h.win.Perform(system.ActionClose)
	case reqActivate:
		a := r.act
		switch a.Action {
		case notify.Reply:
			if txt := strings.TrimSpace(a.Text); txt != "" {
				if m := h.b.Send(a.ID, model.Draft{Text: txt}); m != nil && h.u != nil {
					h.u.upsertMessage(m)
				}
				h.markRead(a.ID)
				break
			}
			fallthrough // Reply with nothing typed opens the chat
		case notify.Open:
			h.openChat = a.ID
			h.show()
		case notify.MarkRead:
			h.markRead(a.ID)
		}
	case reqSwitch:
		h.switchAccount(r.dir)
	case reqAddAccount:
		h.addAccount()
	case reqRedraw:
		if h.u != nil {
			h.u.settings.stale = true
			h.win.Invalidate()
		}
	case reqRestart:
		return h.restart()
	}
	return false
}

// markRead marks a chat read from a notification.
func (h *host) markRead(id string) {
	h.b.Open(id)
	h.notes.read(id)
	if h.u != nil {
		if c := h.u.chatByID(id); c != nil {
			c.Unread = 0
		}
		h.win.Invalidate()
	}
}

// show brings the window up, opening one if there is none, and opens
// openChat in it.
func (h *host) show() {
	if h.win == nil {
		h.openWindow()
		return
	}
	// Perform waits for the window thread, and raising a window that isn't
	// in front makes that thread deliver focus and size events, which wait
	// for this goroutine: both would hang. Only within a frame (deco's
	// buttons) is it safe to wait, since Gio delivers those events re-entrantly.
	go h.win.Perform(system.ActionRaise)
	if h.openChat != "" {
		h.u.openFromNotification(h.openChat)
		h.openChat = ""
	}
	h.win.Invalidate()
}

func (h *host) openWindow() {
	w := new(app.Window)
	opts := h.o.Window
	if h.size != (image.Point{}) {
		opts = append(opts[:len(opts):len(opts)], app.Size(unit.Dp(h.size.X), unit.Dp(h.size.Y)))
	}
	if h.maximized {
		opts = append(opts[:len(opts):len(opts)], app.Maximized.Option())
	}
	w.Option(opts...)
	h.win = w
	h.newUI()
	h.focused = true
	events, ack := make(chan event.Event), make(chan struct{})
	h.events, h.ack = events, ack
	go func() {
		for {
			e := w.Event()
			events <- e
			<-ack // Gio wants each event handled before the next
			if _, ok := e.(app.DestroyEvent); ok {
				return
			}
		}
	}()
	// Once nothing has been drawn for a while, give memory back (see
	// memtrim). Every frame pushes the trim back. Leaving the window trims
	// sooner, even while something on screen still animates.
	h.idle = time.AfterFunc(idleTrim, memtrim.Trim)
	h.away = time.AfterFunc(awayTrim, memtrim.Trim)
	h.away.Stop()
}

// newUI makes the open window's UI, for the open account.
func (h *host) newUI() {
	u := New(hostBackend{Backend: h.b, h: h})
	u.window, u.host = h.win, h
	u.drafts = h.drafts
	h.u = u
	u.accounts = h.accountRows()
	// The window may be there already (another account opened in it).
	u.deco.Maximized = h.maximized
	u.away = h.win != nil && h.events != nil && !h.focused
	// A new UI starts from the stored chats and the current state.
	h.queue = append(h.queue[:0], h.conn)
	if h.syncPct >= 0 && h.conn.State.LoggedIn() {
		h.queue = append(h.queue, model.SyncEvent{Percent: h.syncPct})
	}
	if h.notice != "" {
		h.queue = append(h.queue, model.NoticeEvent{Text: h.notice})
		h.notice = ""
	}
	u.Start(h.win.Invalidate)
	if h.openChat != "" {
		u.openFromNotification(h.openChat)
		h.openChat = ""
	}
}

// windowEvent handles an event of the open window. It reports whether
// the window is gone.
func (h *host) windowEvent(e event.Event) (closed bool, err error) {
	u := h.u
	switch e := e.(type) {
	case app.DestroyEvent:
		u.stopOutgoingTyping()
		return true, e.Err
	case app.ConfigEvent:
		u.deco.Maximized = e.Config.Mode == app.Maximized
		if f := e.Config.Focused && e.Config.Mode != app.Minimized; f != h.focused {
			h.focused = f
			u.away = !f
			if !f {
				u.stopOutgoingTyping()
			}
			if f {
				u.applyTheme()
				h.away.Stop()
			} else {
				h.away.Reset(awayTrim)
			}
		}
		switch e.Config.Mode {
		case app.Windowed:
			h.maximized = false
			if h.pxPerDp > 0 {
				h.size = image.Pt(int(float32(e.Config.Size.X)/h.pxPerDp+.5), int(float32(e.Config.Size.Y)/h.pxPerDp+.5))
			}
		case app.Maximized:
			h.maximized = true
		}
	case app.FrameEvent:
		h.pxPerDp = e.Metric.PxPerDp
		gtx := app.NewContext(&h.ops, e)
		gtx.Now = h.clock.next(gtx.Now)
		u.Layout(gtx)
		e.Frame(gtx.Ops)
		h.idle.Reset(idleTrim)
	default:
		if hwnd := windowHandle(e); hwnd != 0 && hwnd != h.hwnd {
			h.hwnd = hwnd
			desktop.SetWindowIcon(hwnd, appIcon)
			h.enableDrop(hwnd)
		}
	}
	return false, nil
}

// enableDrop lets files be dropped on the window. The handler runs on
// the window's thread, so it only queues the files and asks for a frame.
func (h *host) enableDrop(hwnd uintptr) {
	if h.drops == nil {
		h.drops = make(chan []string, 8)
	}
	win, drops := h.win, h.drops
	redraw := func() { go win.Invalidate() }
	desktop.EnableDrop(hwnd, desktop.DropHandler{
		Over: func(on bool) {
			h.dragging.Store(on)
			redraw()
		},
		Drop: func(paths []string) {
			select {
			case drops <- paths:
			default:
			}
			redraw()
		},
	})
}

// closeWindow lets go of a destroyed window and what only it used.
func (h *host) closeWindow() {
	h.idle.Stop()
	h.away.Stop()
	h.u.stashDraft() // the open chat's composer, for the next window
	h.u.shutdown()
	h.win, h.u, h.events, h.ack, h.queue = nil, nil, nil, nil, nil
	h.hwnd = 0
	h.dragging.Store(false)
	h.ops = op.Ops{}
	h.focused = false
	dropCaches()
	memtrim.Trim()
}

// withAuto returns b with scheduled messages and AFK replies (package
// auto), which the host's backend has already: they go on without a
// window.
func withAuto(b model.Backend) (model.Backend, *auto.Backend) {
	switch x := b.(type) {
	case nil:
		return nil, nil
	case *auto.Backend:
		return x, x
	case hostBackend:
		if a, ok := x.Backend.(*auto.Backend); ok {
			return x, a
		}
	}
	a := auto.Wrap(b, func() time.Time { return timeNow() })
	return a, a
}

// hostBackend is the backend as a window's UI sees it: the host started
// it already, and its events come through the host.
type hostBackend struct {
	model.Backend
	h *host
}

func (hb hostBackend) Start(func())        {}
func (hb hostBackend) Poll() []model.Event { return hb.h.uiEvents() }

// openFromNotification opens a chat on the Chats page.
func (u *UI) openFromNotification(id string) {
	u.applyEvents()
	c := u.chatByID(id)
	if c == nil {
		return
	}
	u.setPage(pageChats)
	u.sidebar.showArchived = c.Archived
	u.open(c)
}

// shutdown stops what plays in a window that is going away.
func (u *UI) shutdown() {
	u.stopVoice()
	u.hideViewer()
	u.status.viewer.close()
	u.players.stopAll()
}

// dropCaches empties the package's caches of what windows drew.
func dropCaches() {
	glyphs = map[glyphKey]*glyphRec{}
	previews.m = nil
	richBlocks.m = nil
	readMoreCuts.m = nil
	icon.FlushCache()
	dropShapes()
}

// appIcon draws the app's icon (the executable's own comes from cmd/winres).
func appIcon(px int) *image.RGBA { return icon.App(px) }

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}
