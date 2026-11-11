//go:build linux

package desktop

import (
	"bytes"
	"testing"
	"time"
)

// TestWaylandWire checks the messages the idle watcher sends and reads.
func TestWaylandWire(t *testing.T) {
	var buf bytes.Buffer
	if err := sendMsg(&buf, wlRegistry, 0, u32(7), str("wl_seat"), u32(1), u32(wlSeat)); err != nil {
		t.Fatal(err)
	}
	if buf.Len()%4 != 0 {
		t.Fatalf("message of %d bytes isn't padded", buf.Len())
	}
	id, op, body, err := readMsg(&buf)
	if err != nil || id != wlRegistry || op != 0 {
		t.Fatalf("read id %d, op %d, %v", id, op, err)
	}
	if got := len(body); got != 4+4+8+4+4 {
		t.Fatalf("body of %d bytes", got)
	}
	name, rest := wlString(body[4:])
	if name != "wl_seat" || len(rest) != 8 {
		t.Fatalf("string %q, %d bytes after it", name, len(rest))
	}
}

// TestWaylandIdle asks the session's compositor, when there is one.
func TestWaylandIdle(t *testing.T) {
	conn, err := waylandIdle()
	if err != nil {
		t.Skip(err)
	}
	defer conn.Close()
	// A request the compositor refuses comes back as wl_display.error.
	conn.SetDeadline(time.Now().Add(300 * time.Millisecond))
	for {
		id, op, body, err := readMsg(conn)
		if err != nil {
			return // nothing more: it took the requests
		}
		if id == wlDisplay && op == 0 {
			t.Fatalf("the compositor refused a request: %q", body)
		}
	}
}
