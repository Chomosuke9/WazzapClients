package remote

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chomosuke9/wazzapclients/internal/mock"
	"github.com/chomosuke9/wazzapclients/internal/model"
)

const testToken = "secret-token"

// testHost is the app around a demo backend. Do runs requests one at a
// time, as the UI goroutine would.
type testHost struct {
	mu   sync.Mutex
	b    *mock.Backend
	conn model.ConnEvent
	sent []*model.Message
	read []string
}

func (h *testHost) Backend() model.Backend { return h.b }
func (h *testHost) Conn() model.ConnEvent  { return h.conn }
func (h *testHost) Sent(m *model.Message)  { h.sent = append(h.sent, m) }
func (h *testHost) Read(id string)         { h.read = append(h.read, id) }

func (h *testHost) do(f func()) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	f()
	return nil
}

func newTest(t *testing.T) (*Server, *testHost) {
	t.Helper()
	h := &testHost{b: mock.New(), conn: model.ConnEvent{State: model.StateOnline, Me: "Me"}}
	s := newServer(Options{Token: testToken, Version: "v1.0.0", Do: h.do, Host: h})
	t.Cleanup(func() { close(s.stop) })
	return s, h
}

// do sends a request with the token and decodes the reply into v.
func do(t *testing.T, s *Server, method, path, body string, v any) int {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(w, r)
	if v != nil {
		if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
			t.Fatalf("%s %s: %v in %q", method, path, err, w.Body)
		}
	}
	return w.Code
}

func TestAuth(t *testing.T) {
	s, _ := newTest(t)
	for _, h := range []string{"", "Bearer", "Bearer wrong", "Basic " + testToken, testToken} {
		r := httptest.NewRequest("GET", "/v1/ping", nil)
		if h != "" {
			r.Header.Set("Authorization", h)
		}
		w := httptest.NewRecorder()
		s.srv.Handler.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("Authorization %q: got %d, want 401", h, w.Code)
		}
	}
	var ping map[string]string
	if code := do(t, s, "GET", "/v1/ping", "", &ping); code != http.StatusOK {
		t.Fatalf("ping: %d", code)
	}
	if ping["state"] != "online" || ping["me"] != "Me" || ping["version"] != "v1.0.0" || ping["cursor"] == "" {
		t.Errorf("ping = %v", ping)
	}
	if s.Status().LastSeen.IsZero() {
		t.Error("LastSeen not set by a request with the token")
	}
	s.SetToken("new")
	if code := do(t, s, "GET", "/v1/ping", "", nil); code != http.StatusUnauthorized {
		t.Errorf("old token after SetToken: %d", code)
	}
}

type chatPage struct {
	Chats  []Chat `json:"chats"`
	More   bool   `json:"more"`
	Cursor string `json:"cursor"`
}

func TestChats(t *testing.T) {
	s, h := newTest(t)
	want := listed(h.b.Chats())
	if len(want) < 4 {
		t.Fatalf("demo lists %d chats", len(want))
	}
	for _, c := range want {
		if c.Archived {
			t.Errorf("archived chat %s listed", c.ID)
		}
	}
	var p chatPage
	if code := do(t, s, "GET", "/v1/chats?limit=3", "", &p); code != http.StatusOK {
		t.Fatalf("chats: %d", code)
	}
	if len(p.Chats) != 3 || !p.More || p.Cursor == "" {
		t.Fatalf("first page: %d chats, more %v, cursor %q", len(p.Chats), p.More, p.Cursor)
	}
	var got []string
	for {
		for _, c := range p.Chats {
			got = append(got, c.ID)
		}
		if !p.More {
			break
		}
		p = chatPage{}
		do(t, s, "GET", "/v1/chats?limit=3&after="+got[len(got)-1], "", &p)
	}
	if len(got) != len(want) {
		t.Fatalf("paged through %d chats, want %d", len(got), len(want))
	}
	for i, c := range want {
		if got[i] != c.ID {
			t.Errorf("chat %d is %s, want %s", i, got[i], c.ID)
		}
	}
	if code := do(t, s, "GET", "/v1/chats?limit=0", "", nil); code != http.StatusBadRequest {
		t.Errorf("limit=0: %d", code)
	}
	if code := do(t, s, "GET", "/v1/chats?after=nobody", "", nil); code != http.StatusNotFound {
		t.Errorf("after an unknown chat: %d", code)
	}
}

type msgPage struct {
	Messages []Message `json:"messages"`
	More     bool      `json:"more"`
}

// busiest returns the listed chat with the most messages.
func busiest(t *testing.T, h *testHost) (string, []*model.Message) {
	var id string
	var msgs []*model.Message
	for _, c := range listed(h.b.Chats()) {
		if m := h.b.Messages(c.ID, 1000); len(m) > len(msgs) {
			id, msgs = c.ID, m
		}
	}
	if len(msgs) < 3 {
		t.Fatalf("no demo chat has 3 messages")
	}
	return id, msgs
}

func TestMessages(t *testing.T) {
	s, h := newTest(t)
	id, all := busiest(t, h)
	var p msgPage
	if code := do(t, s, "GET", "/v1/chats/"+id+"/messages?limit=2", "", &p); code != http.StatusOK {
		t.Fatalf("messages: %d", code)
	}
	n := len(all)
	if len(p.Messages) != 2 || !p.More || p.Messages[0].ID != all[n-2].ID || p.Messages[1].ID != all[n-1].ID {
		t.Fatalf("newest page = %+v, more %v", p.Messages, p.More)
	}
	var got []string
	for {
		ids := make([]string, len(p.Messages))
		for i, m := range p.Messages {
			ids[i] = m.ID
		}
		got = append(ids, got...)
		if !p.More {
			break
		}
		before := p.Messages[0].ID
		p = msgPage{}
		do(t, s, "GET", "/v1/chats/"+id+"/messages?limit=2&before="+before, "", &p)
	}
	if len(got) != n {
		t.Fatalf("paged through %d messages, want %d", len(got), n)
	}
	var one struct{ Message Message }
	if code := do(t, s, "GET", "/v1/chats/"+id+"/messages/"+all[0].ID, "", &one); code != http.StatusOK || one.Message.ID != all[0].ID {
		t.Errorf("one message: %d %+v", code, one.Message)
	}
	if code := do(t, s, "GET", "/v1/chats/"+id+"/messages/nope", "", nil); code != http.StatusNotFound {
		t.Errorf("unknown message: %d", code)
	}
}

func TestSend(t *testing.T) {
	s, h := newTest(t)
	id, all := busiest(t, h)
	reply := all[len(all)-1]
	body := `{"text": "  On my way  ", "replyTo": "` + reply.ID + `", "nonce": "n1"}`
	var res struct{ Message Message }
	if code := do(t, s, "POST", "/v1/chats/"+id+"/messages", body, &res); code != http.StatusCreated {
		t.Fatalf("send: %d", code)
	}
	if res.Message.Text != "On my way" || !res.Message.Mine || res.Message.Quote == nil || res.Message.Quote.ID != reply.ID {
		t.Errorf("sent %+v", res.Message)
	}
	if len(h.sent) != 1 || len(h.read) != 1 || h.read[0] != id {
		t.Errorf("host told of %d sent, read %v", len(h.sent), h.read)
	}
	// The phone sending it again doesn't send it twice.
	var again struct{ Message Message }
	do(t, s, "POST", "/v1/chats/"+id+"/messages", body, &again)
	if len(h.sent) != 1 || again.Message.ID != res.Message.ID {
		t.Errorf("the same nonce sent again: %d sends", len(h.sent))
	}
	if got := h.b.Messages(id, 1); len(got) != 1 || got[0].Text != "On my way" {
		t.Errorf("backend's newest message: %+v", got)
	}
	for _, c := range []struct {
		path, body string
		code       int
	}{
		{"/v1/chats/" + id + "/messages", `{"text": "   "}`, http.StatusBadRequest},
		{"/v1/chats/" + id + "/messages", `not json`, http.StatusBadRequest},
		{"/v1/chats/" + id + "/messages", `{"text": "` + strings.Repeat("a", sendLimit+1) + `"}`, http.StatusBadRequest},
		{"/v1/chats/nobody/messages", `{"text": "hi"}`, http.StatusNotFound},
	} {
		if code := do(t, s, "POST", c.path, c.body, nil); code != c.code {
			t.Errorf("POST %s %.20q: %d, want %d", c.path, c.body, code, c.code)
		}
	}
	h.conn.State = model.StateQR
	if code := do(t, s, "POST", "/v1/chats/"+id+"/messages", `{"text": "hi"}`, nil); code != http.StatusServiceUnavailable {
		t.Errorf("send while logged out: %d", code)
	}
}

func TestRead(t *testing.T) {
	s, h := newTest(t)
	if code := do(t, s, "POST", "/v1/chats/c1/read", "", nil); code != http.StatusOK || len(h.read) != 1 || h.read[0] != "c1" {
		t.Errorf("read: %d %v", code, h.read)
	}
}

type eventPage struct {
	Events []Event `json:"events"`
	Cursor string  `json:"cursor"`
	Reset  bool    `json:"reset"`
}

func TestEvents(t *testing.T) {
	s, _ := newTest(t)
	var ping map[string]string
	do(t, s, "GET", "/v1/ping", "", &ping)
	cur := ping["cursor"]

	var p eventPage
	do(t, s, "GET", "/v1/events?wait=0&since="+cur, "", &p)
	if len(p.Events) != 0 || p.Reset || p.Cursor != cur {
		t.Fatalf("nothing new: %+v", p)
	}

	// A request waiting gets the event as it comes.
	done := make(chan eventPage)
	go func() {
		var p eventPage
		do(t, s, "GET", "/v1/events?wait=10&since="+cur, "", &p)
		done <- p
	}()
	time.Sleep(50 * time.Millisecond)
	s.Event(model.MessageEvent{Msg: &model.Message{ID: "m1", ChatID: "c1", Text: "hi", Time: time.Unix(100, 0)}, New: true})
	s.Event(model.TypingEvent{}) // not for the watch
	s.Event(model.ReceiptEvent{ChatID: "c1", IDs: []string{"m0"}, Receipt: model.Read})
	select {
	case p = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("waiting request never answered")
	}
	if len(p.Events) == 0 || p.Events[0].Type != "message" || p.Events[0].Message.Text != "hi" || !p.Events[0].New {
		t.Fatalf("woken with %+v", p)
	}
	next := p.Cursor
	p = eventPage{}
	do(t, s, "GET", "/v1/events?wait=0&since="+cur, "", &p)
	if len(p.Events) != 2 || p.Events[1].Type != "receipt" || p.Events[1].Status != "read" {
		t.Fatalf("since the start: %+v", p.Events)
	}
	if next == cur {
		t.Error("cursor didn't move")
	}

	for _, bad := range []string{"", "nonsense", "00000000-0", cur + "9"} {
		p = eventPage{}
		do(t, s, "GET", "/v1/events?wait=0&since="+bad, "", &p)
		if !p.Reset {
			t.Errorf("since %q: no reset", bad)
		}
	}
	// A watch too far behind reloads.
	for range eventKeep + 1 {
		s.Event(model.ConnEvent{State: model.StateOnline})
	}
	p = eventPage{}
	do(t, s, "GET", "/v1/events?wait=0&since="+cur, "", &p)
	if !p.Reset {
		t.Error("a cursor older than the buffer didn't reset")
	}
}

func TestClip(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
		cut  bool
	}{
		{"hello", 5, "hello", false},
		{"hello world", 5, "hello…", true},
		{"héllo wörld", 7, "héllo w…", true},
		{"héllo wörld", 6, "héllo…", true},
		{"⁨@Ann" + string(model.MentionNotifies) + "⁩ hi", 20, "@Ann hi", false},
		{"long", -1, "long", false},
	} {
		got, cut := clip(c.in, c.n)
		if got != c.want || cut != c.cut {
			t.Errorf("clip(%q, %d) = %q, %v; want %q, %v", c.in, c.n, got, cut, c.want, c.cut)
		}
	}
}
