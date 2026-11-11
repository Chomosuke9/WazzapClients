//go:build linux

package desktop

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// idleTimeout is how long without input a Wayland compositor waits before
// it says the user is idle. The UI's idleAfter (a minute) is the only
// length IdleTime is compared with, so it's the one asked for.
const idleTimeout = time.Minute

var idle struct {
	once sync.Once
	sync.Mutex
	wayland bool      // a compositor reports through ext-idle-notify
	since   time.Time // when the user went idle, zero while they aren't
	bus     *dbus.Conn
}

// IdleTime is how long the user hasn't touched the keyboard or mouse.
//
// A Wayland compositor says when the user has been idle for idleTimeout
// and when they're back (ext-idle-notify-v1: Hyprland, Sway, KDE, GNOME 45
// and later), so until then it's 0. Otherwise GNOME's and KDE's session
// services are asked. A system with neither is never idle.
func IdleTime() time.Duration {
	idle.once.Do(func() {
		ready := make(chan bool, 1)
		go watchIdle(ready)
		idle.wayland = <-ready
		if !idle.wayland {
			idle.bus, _ = dbus.ConnectSessionBus()
		}
	})
	idle.Lock()
	defer idle.Unlock()
	if idle.wayland {
		if idle.since.IsZero() {
			return 0
		}
		return time.Since(idle.since)
	}
	if idle.bus == nil {
		return 0
	}
	var ms uint64
	if idle.bus.Object("org.gnome.Mutter.IdleMonitor", "/org/gnome/Mutter/IdleMonitor/Core").
		Call("org.gnome.Mutter.IdleMonitor.GetIdletime", 0).Store(&ms) == nil {
		return time.Duration(ms) * time.Millisecond
	}
	var ms32 uint32
	if idle.bus.Object("org.freedesktop.ScreenSaver", "/org/freedesktop/ScreenSaver").
		Call("org.freedesktop.ScreenSaver.GetSessionIdleTime", 0).Store(&ms32) == nil {
		return time.Duration(ms32) * time.Millisecond
	}
	return 0
}

// watchIdle follows the compositor's idle notifications on a Wayland
// connection of its own (Gio keeps its to itself), speaking just enough of
// the wire protocol: every message is an object ID, a size and opcode, and
// 32-bit arguments in the machine's byte order. It sends ready whether the
// compositor has the protocol, then goes on reading for good.
func watchIdle(ready chan<- bool) {
	conn, err := waylandIdle()
	if err != nil {
		ready <- false
		return
	}
	ready <- true
	defer conn.Close()
	for {
		id, op, _, err := readMsg(conn)
		if err != nil {
			// The compositor is gone, and the app's window with it.
			idle.Lock()
			idle.since = time.Time{}
			idle.Unlock()
			return
		}
		if id != wlNotification {
			continue
		}
		idle.Lock()
		switch op {
		case 0: // idled
			idle.since = time.Now().Add(-idleTimeout)
		case 1: // resumed
			idle.since = time.Time{}
		}
		idle.Unlock()
	}
}

// The IDs this client gives its objects; the display is always 1.
const (
	wlDisplay = 1 + iota
	wlRegistry
	wlSync
	wlSeat
	wlNotifier
	wlNotification
)

// waylandIdle connects to the compositor and asks to be told when the
// user's seat goes idle.
func waylandIdle() (net.Conn, error) {
	name := os.Getenv("WAYLAND_DISPLAY")
	if name == "" {
		return nil, errors.New("no Wayland display")
	}
	if !filepath.IsAbs(name) {
		name = filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), name)
	}
	conn, err := net.Dial("unix", name)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (net.Conn, error) {
		conn.Close()
		return nil, err
	}
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	// wl_display.get_registry, then wl_display.sync: its callback comes
	// once the registry has listed every global.
	sendMsg(conn, wlDisplay, 1, u32(wlRegistry))
	if err := sendMsg(conn, wlDisplay, 0, u32(wlSync)); err != nil {
		return fail(err)
	}
	var seat, notifier uint32
	for done := false; !done; {
		id, op, body, err := readMsg(conn)
		if err != nil {
			return fail(err)
		}
		switch {
		case id == wlSync:
			done = true
		case id == wlRegistry && op == 0 && len(body) >= 8: // global
			global := binary.NativeEndian.Uint32(body)
			switch iface, _ := wlString(body[4:]); iface {
			case "wl_seat":
				if seat == 0 {
					seat = global
				}
			case "ext_idle_notifier_v1":
				notifier = global
			}
		}
	}
	if seat == 0 || notifier == 0 {
		return fail(errors.New("no ext_idle_notifier_v1"))
	}
	// wl_registry.bind names the interface and version of the new object.
	bind := func(global uint32, iface string, id uint32) error {
		return sendMsg(conn, wlRegistry, 0, u32(global), str(iface), u32(1), u32(id))
	}
	bind(seat, "wl_seat", wlSeat)
	bind(notifier, "ext_idle_notifier_v1", wlNotifier)
	// ext_idle_notifier_v1.get_idle_notification(new id, timeout, seat).
	err = sendMsg(conn, wlNotifier, 1, u32(wlNotification), u32(uint32(idleTimeout/time.Millisecond)), u32(wlSeat))
	if err != nil {
		return fail(err)
	}
	conn.SetDeadline(time.Time{})
	return conn, nil
}

func u32(v uint32) []byte { return binary.NativeEndian.AppendUint32(nil, v) }

// str is a string argument: its length with the NUL, then the bytes padded
// to a multiple of four.
func str(s string) []byte {
	n := len(s) + 1
	b := binary.NativeEndian.AppendUint32(nil, uint32(n))
	b = append(b, s...)
	return append(b, make([]byte, (n+3)&^3-len(s))...)
}

func wlString(b []byte) (string, []byte) {
	if len(b) < 4 {
		return "", nil
	}
	n := int(binary.NativeEndian.Uint32(b))
	if n == 0 || 4+n > len(b) {
		return "", nil
	}
	return string(b[4 : 4+n-1]), b[4+(n+3)&^3:]
}

func sendMsg(w io.Writer, id uint32, op uint16, args ...[]byte) error {
	msg := make([]byte, 8)
	for _, a := range args {
		msg = append(msg, a...)
	}
	binary.NativeEndian.PutUint32(msg, id)
	binary.NativeEndian.PutUint32(msg[4:], uint32(len(msg))<<16|uint32(op))
	_, err := w.Write(msg)
	return err
}

func readMsg(r io.Reader) (id uint32, op uint16, body []byte, err error) {
	var head [8]byte
	if _, err = io.ReadFull(r, head[:]); err != nil {
		return
	}
	id = binary.NativeEndian.Uint32(head[:])
	v := binary.NativeEndian.Uint32(head[4:])
	size := int(v >> 16)
	if size < 8 {
		return 0, 0, nil, errors.New("wayland: short message")
	}
	body = make([]byte, size-8)
	_, err = io.ReadFull(r, body)
	return id, uint16(v), body, err
}
