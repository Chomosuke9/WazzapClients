package ui

import (
	"bytes"
	"net/http"
	"os/signal"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

// headlessOut is what the headless mode wrote, read under its lock.
func headlessOut(h *host, buf *bytes.Buffer) string {
	h.headless.mu.Lock()
	defer h.headless.mu.Unlock()
	return buf.String()
}

// Linking from the terminal: the QR code drawn in text, what happens
// next, and a line typed.
func TestHeadlessLinking(t *testing.T) {
	var buf bytes.Buffer
	b := mock.New()
	h := newHost(b, Options{Headless: &Headless{Out: &buf, In: strings.NewReader("")}})
	defer signal.Stop(h.startHeadless())
	hh := h.headless

	h.conn = model.ConnEvent{State: model.StateQR, QR: "2@abc,def,ghi"}
	hh.conn(h.conn)
	if out := headlessOut(h, &buf); !strings.Contains(out, "█") || !strings.Contains(out, "Linked devices") || !strings.Contains(out, "phone number") {
		t.Fatalf("no QR code:\n%s", out)
	}
	n := len(headlessOut(h, &buf))
	hh.conn(h.conn) // the same code again draws nothing
	if len(headlessOut(h, &buf)) != n {
		t.Fatal("the same QR code drawn twice")
	}
	hh.line("12") // neither a number nor empty
	if !strings.Contains(headlessOut(h, &buf), "Type a phone number") {
		t.Fatal("no help for a line it doesn't take")
	}

	h.conn = model.ConnEvent{State: model.StateQRExpired}
	hh.conn(h.conn)
	if !strings.Contains(headlessOut(h, &buf), "ran out") {
		t.Fatal("expired codes not said")
	}
	hh.line("")
	if !strings.Contains(headlessOut(h, &buf), "new QR codes") {
		t.Fatal("Enter doesn't ask for new codes")
	}

	h.conn = model.ConnEvent{State: model.StateQR, QR: "2@new"}
	hh.conn(h.conn)
	h.conn = model.ConnEvent{State: model.StateConnecting}
	hh.conn(h.conn)
	h.conn = model.ConnEvent{State: model.StateOnline, Me: "Me Myself"}
	hh.conn(h.conn)
	if out := headlessOut(h, &buf); !strings.Contains(out, "Linked. Connecting") || !strings.Contains(out, "Online as Me Myself") {
		t.Fatalf("linking not said:\n%s", out)
	}
	hh.line("628123456789")
	if !strings.Contains(headlessOut(h, &buf), "linked already") {
		t.Fatal("a pairing code asked for once linked")
	}
}

// -phone links with a pairing code instead of the QR code.
func TestHeadlessPhone(t *testing.T) {
	var buf bytes.Buffer
	h := newHost(mock.New(), Options{Headless: &Headless{Out: &buf, Phone: "628123456789"}})
	defer signal.Stop(h.startHeadless())
	h.headless.conn(model.ConnEvent{State: model.StateQR, QR: "2@abc"})
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(headlessOut(h, &buf), "WAZZ-DEMO") {
		if time.Now().After(deadline) {
			t.Fatalf("no pairing code:\n%s", headlessOut(h, &buf))
		}
		time.Sleep(5 * time.Millisecond)
	}
	if strings.Contains(headlessOut(h, &buf), "█") {
		t.Fatal("a QR code drawn with -phone")
	}
}

// The server listens where it's told with the token it's given, and a
// token made here stays the same from one start to the next.
func TestHeadlessServer(t *testing.T) {
	b := mock.New()
	port := freePort(t)
	start := func(token string) (*host, string) {
		var buf bytes.Buffer
		h := newHost(b, Options{Headless: &Headless{Out: &buf, Hosts: []string{"127.0.0.1"}, Port: port, Token: token}})
		q := h.startHeadless()
		t.Cleanup(func() { signal.Stop(q) })
		h.applyRemote()
		h.headless.started()
		go func() {
			for f := range h.remoteCalls {
				f()
			}
		}()
		return h, headlessOut(h, &buf)
	}
	h, out := start("")
	tok := b.Pref(prefRemoteToken)
	if tok == "" || !strings.Contains(out, tok) || !strings.Contains(out, "http://127.0.0.1:"+strconv.Itoa(port)) {
		t.Fatalf("token %q, out:\n%s", tok, out)
	}
	get := func(token string) int {
		req, _ := http.NewRequest("GET", "http://127.0.0.1:"+strconv.Itoa(port)+"/v1/ping", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	if code := get(tok); code != http.StatusOK {
		t.Fatalf("ping: %d", code)
	}
	h.stopRemote()
	close(h.remoteCalls)

	h, _ = start("")
	if b.Pref(prefRemoteToken) != tok || get(tok) != http.StatusOK {
		t.Fatal("the token changed from one start to the next")
	}
	h.stopRemote()
	close(h.remoteCalls)

	h, _ = start("mine")
	if get("mine") != http.StatusOK || get(tok) != http.StatusUnauthorized || b.Pref(prefRemoteToken) != "mine" {
		t.Fatal("-token not used and kept")
	}
	h.stopRemote()
	close(h.remoteCalls)
}
