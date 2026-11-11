//go:build linux

package notify

import (
	"errors"
	"html"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
)

// Linux notifications go to the desktop's notification server over D-Bus
// (the Desktop Notifications Specification's org.freedesktop.Notifications,
// which GNOME, KDE and the notification daemons of other desktops all
// serve). It gives each notification a number, which replaces or closes it
// later, and sends a signal when one is clicked or closed.

// ErrUnsupported reports a session without a notification server.
var ErrUnsupported = errors.New("notify: notifications aren't supported")

const (
	noteDest  = "org.freedesktop.Notifications"
	notePath  = dbus.ObjectPath("/org/freedesktop/Notifications")
	noteIface = "org.freedesktop.Notifications"

	// The keys of a notification's actions. "default" is a click on the
	// notification itself, and "inline-reply" the reply box of servers
	// that have one (KDE), whose text comes in NotificationReplied.
	actOpen  = "default"
	actReply = "inline-reply"
	actRead  = "read"
)

var server struct {
	sync.Mutex
	conn *dbus.Conn
	opts Options
	icon string // the app's icon, a file
	// What the server can do: buttons, markup in the body (which then
	// must be escaped), a reply box.
	actions, markup, reply bool
	// The server's number of each notification still shown, by ID, and
	// back.
	shown map[string]uint32
	ids   map[uint32]string
	// The activation token the server sent ahead of a click, by number.
	tokens map[uint32]string
	jobs   chan func()
}

// Init connects to the notification server.
func Init(o Options) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return ErrUnsupported
	}
	var caps []string
	obj := conn.Object(noteDest, notePath)
	if err := obj.Call(noteIface+".GetCapabilities", 0).Store(&caps); err != nil {
		conn.Close()
		return ErrUnsupported
	}
	s := &server
	s.Lock()
	defer s.Unlock()
	s.conn, s.opts = conn, o
	s.shown, s.ids, s.tokens = map[string]uint32{}, map[uint32]string{}, map[uint32]string{}
	for _, c := range caps {
		switch c {
		case "actions":
			s.actions = true
		case "body-markup":
			s.markup = true
		case "inline-reply":
			s.reply = true
		}
	}
	s.icon = imageFile(o.Dir, o.Icon)
	go pruneImages(o.Dir)

	conn.AddMatchSignal(dbus.WithMatchInterface(noteIface), dbus.WithMatchObjectPath(notePath))
	signals := make(chan *dbus.Signal, 16)
	conn.Signal(signals)
	go listen(signals)
	// Calls wait for the server's answer, so they're made off the
	// caller's goroutine, in order.
	jobs := make(chan func(), 32)
	s.jobs = jobs
	go func() {
		for job := range jobs {
			job()
		}
	}()
	return nil
}

// listen hands clicks to Options.Activate and forgets the notifications
// the server closed.
func listen(signals <-chan *dbus.Signal) {
	for sig := range signals {
		if len(sig.Body) == 0 {
			continue
		}
		num, ok := sig.Body[0].(uint32)
		if !ok {
			continue
		}
		arg := ""
		if len(sig.Body) > 1 {
			arg, _ = sig.Body[1].(string)
		}
		s := &server
		s.Lock()
		id, mine := s.ids[num]
		activate := s.opts.Activate
		token := s.tokens[num]
		delete(s.tokens, num)
		if mine && strings.HasSuffix(sig.Name, ".ActivationToken") {
			s.tokens[num] = arg
		}
		if mine && strings.HasSuffix(sig.Name, ".NotificationClosed") {
			delete(s.ids, num)
			if s.shown[id] == num {
				delete(s.shown, id)
			}
		}
		s.Unlock()
		if !mine || activate == nil {
			continue
		}
		switch strings.TrimPrefix(sig.Name, noteIface+".") {
		case "ActionInvoked":
			switch arg {
			case actOpen:
				go activate(Activation{ID: id, Action: Open, Token: token})
			case actRead:
				go activate(Activation{ID: id, Action: MarkRead})
			}
		case "NotificationReplied":
			if text := strings.TrimSpace(arg); text != "" {
				go activate(Activation{ID: id, Action: Reply, Text: text})
			}
		}
	}
}

// queue runs job after the ones before it. It never blocks: a flood drops
// the extra ones.
func queue(job func()) {
	s := &server
	s.Lock()
	jobs := s.jobs
	s.Unlock()
	if jobs == nil {
		return
	}
	select {
	case jobs <- job:
	default:
	}
}

// Show shows n, in place of a notification with the same ID that is still
// shown. It returns at once; failures are dropped.
func Show(n Notification) { queue(func() { show(n) }) }

// Remove takes back the notification with this ID, if it's still shown.
func Remove(id string) {
	queue(func() {
		s := &server
		s.Lock()
		conn, num := s.conn, s.shown[id]
		s.Unlock()
		if conn != nil && num != 0 {
			conn.Object(noteDest, notePath).Call(noteIface+".CloseNotification", 0, num)
		}
	})
}

// Close lets go of the notification server. Notifications stay as they
// are, but nothing hears their clicks any more.
func Close() {
	s := &server
	s.Lock()
	conn, jobs := s.conn, s.jobs
	s.conn, s.jobs = nil, nil
	s.Unlock()
	if jobs != nil {
		close(jobs)
	}
	if conn != nil {
		conn.Close()
	}
}

func show(n Notification) {
	s := &server
	s.Lock()
	conn, opts, icon := s.conn, s.opts, s.icon
	hasActions, markup, reply := s.actions, s.markup, s.reply
	old := s.shown[n.ID]
	s.Unlock()
	if conn == nil {
		return
	}
	body := n.Body
	if n.Footer != "" {
		body = strings.TrimSpace(body + "\n" + n.Footer)
	}
	if markup {
		body = html.EscapeString(body)
	}
	hints := map[string]dbus.Variant{
		// The desktop file, which names the app and gives its icon.
		"desktop-entry": dbus.MakeVariant("wazzapclients"),
		"category":      dbus.MakeVariant("im.received"),
	}
	if p := imageFile(opts.Dir, n.Image); p != "" {
		hints["image-path"] = dbus.MakeVariant(p)
	}
	if n.Silent {
		hints["suppress-sound"] = dbus.MakeVariant(true)
	} else {
		hints["sound-name"] = dbus.MakeVariant("message-new-instant")
	}
	actions := []string{}
	if hasActions {
		actions = append(actions, actOpen, "Open")
		if n.Reply {
			if reply {
				actions = append(actions, actReply, "Reply")
				hints["x-kde-reply-placeholder-text"] = dbus.MakeVariant("Type a message")
			}
			actions = append(actions, actRead, "Mark as read")
		}
	}
	var num uint32
	// -1 leaves how long it stays to the server.
	err := conn.Object(noteDest, notePath).Call(noteIface+".Notify", 0,
		opts.Name, old, icon, n.Title, body, actions, hints, int32(-1)).Store(&num)
	if err != nil || num == 0 {
		return
	}
	s.Lock()
	if old != 0 && old != num {
		delete(s.ids, old)
	}
	s.shown[n.ID], s.ids[num] = num, n.ID
	s.Unlock()
}
