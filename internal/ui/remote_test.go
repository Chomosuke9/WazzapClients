package ui

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// freePort returns a port nothing listens on.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// remoteRequest sends a request to the host's server and serves the host's
// side of it, as Run would, until the answer comes.
func remoteRequest(t *testing.T, h *host, method, path, body string) (int, string) {
	t.Helper()
	type result struct {
		code int
		body string
		err  error
	}
	res := make(chan result, 1)
	go func() {
		r, _ := http.NewRequest(method, "http://127.0.0.1:"+strconv.Itoa(h.remoteCfg.port)+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+h.b.Pref(prefRemoteToken))
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			res <- result{err: err}
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		res <- result{code: resp.StatusCode, body: string(b)}
	}()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case f := <-h.remoteCalls:
			f()
		case r := <-res:
			if r.err != nil {
				t.Fatalf("%s %s: %v", method, path, r.err)
			}
			return r.code, r.body
		case <-timeout:
			t.Fatalf("%s %s: no answer", method, path)
		}
	}
}

func TestRemoteAccess(t *testing.T) {
	h := newAccountsHost(t)
	h.b.SetPref(prefRemotePort, strconv.Itoa(freePort(t)))
	h.applyRemote()
	if h.remote != nil {
		t.Fatal("the server runs before Smartwatch access is on")
	}
	setExtra(h.b, prefRemote, true)
	h.applyRemote()
	defer h.stopRemote()
	if h.remote == nil || h.b.Pref(prefRemoteToken) == "" {
		t.Fatalf("turned on: server %v, token %q", h.remote, h.b.Pref(prefRemoteToken))
	}

	code, body := remoteRequest(t, h, "GET", "/v1/chats?limit=5", "")
	var page struct {
		Chats []struct{ ID string }
	}
	if err := json.Unmarshal([]byte(body), &page); code != http.StatusOK || err != nil || len(page.Chats) == 0 {
		t.Fatalf("chats: %d %q", code, body)
	}
	chat := page.Chats[0].ID

	code, body = remoteRequest(t, h, "POST", "/v1/chats/"+chat+"/messages", `{"text": "From the watch", "nonce": "a"}`)
	if code != http.StatusCreated {
		t.Fatalf("send: %d %q", code, body)
	}
	if ms := h.b.Messages(chat, 1); len(ms) != 1 || ms[0].Text != "From the watch" {
		t.Errorf("the backend's newest message: %+v", ms)
	}

	// A new token shuts the old one out.
	old := h.b.Pref(prefRemoteToken)
	h.b.SetPref(prefRemoteToken, newRemoteToken())
	h.applyRemote()
	r, _ := http.NewRequest("GET", "http://127.0.0.1:"+strconv.Itoa(h.remoteCfg.port)+"/v1/ping", nil)
	r.Header.Set("Authorization", "Bearer "+old)
	if resp, err := http.DefaultClient.Do(r); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("old token: %v %v", resp, err)
	} else {
		resp.Body.Close()
	}

	setExtra(h.b, prefRemote, false)
	h.applyRemote()
	if h.remote != nil {
		t.Fatal("still running once turned off")
	}
	if _, err := http.Get("http://127.0.0.1:" + strconv.Itoa(h.remoteCfg.port) + "/v1/ping"); err == nil {
		t.Error("still listening once turned off")
	}
}
