// Package remote serves the open account's chats over HTTP to the
// watch app (github.com/Chomosuke9/WazzapWatch, Amazfit, Zepp OS), whose
// side service on the phone reaches this computer through Tailscale. It knows only internal/model:
// every request runs on the app's UI goroutine (Options.Do), so the
// backend is never called from two goroutines at once.
//
// The API, under /v1, with "Authorization: Bearer <token>" on every request:
//
//	GET  /v1/ping                              version, connection state, events cursor
//	GET  /v1/chats?limit=20&after=<chat>        the chat list, a page at a time
//	GET  /v1/chats/{chat}/messages?limit=15&before=<id>   newest first page, then older
//	GET  /v1/chats/{chat}/messages/{id}         one message with all of its text
//	POST /v1/chats/{chat}/messages              send {"text", "replyTo", "nonce"}
//	POST /v1/chats/{chat}/messages/{id}/react   react {"emoji"}; "" takes yours back
//	POST /v1/chats/{chat}/read                  mark the chat read
//	GET  /v1/events?since=<cursor>&wait=25      what changed since cursor (long poll)
//	GET  /v1/search?q=<text>&limit=15           chats by name and messages by text
//	GET  /v1/chats/{chat}/photo/version         which profile picture the chat has
//	GET  /v1/chats/{chat}/photo?size=58         the profile picture, a PNG circle
//	GET  /v1/chats/{chat}/messages/{id}/image?w=400&h=400   a photo or sticker, a PNG
package remote

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

// DefaultPort is the port the server listens on unless told otherwise.
const DefaultPort = 8642

// Host is the app the server serves. Its methods are called inside
// Options.Do only, on the UI goroutine.
type Host interface {
	// Backend is the open account's backend.
	Backend() model.Backend
	// Conn is the open account's latest connection state.
	Conn() model.ConnEvent
	// Sent is told about a message the watch sent, for the window to show.
	Sent(m *model.Message)
	// Read marks a chat read, as a notification's Mark as read does.
	Read(chatID string)
}

// Options configure a Server.
type Options struct {
	Port int
	// AllNetworks listens on every network instead of this computer and
	// its Tailscale addresses alone.
	AllNetworks bool
	Token       string
	Version     string
	// Do runs f on the UI goroutine and waits for it. It returns an error
	// when f couldn't run in time (it may still run later).
	Do   func(f func()) error
	Host Host
}

// Server is a running API server.
type Server struct {
	o    Options
	srv  *http.Server
	ev   *events
	stop chan struct{}

	mu       sync.Mutex
	token    string
	lns      map[string]net.Listener // by address
	err      string                  // the last address that couldn't be listened on, and why
	lastSeen time.Time               // the last request with the right token

	// The last messages sent by nonce, so a request sent again (the
	// phone retrying) doesn't send twice. UI goroutine only.
	sent      map[string]Message
	sentOrder []string

	// Requests waiting for a backend event (images.go). UI goroutine only.
	waiters []*waiter
}

// Status is what the settings page shows about a server.
type Status struct {
	// Addrs are the addresses listened on, as host:port; ":port" means
	// every network.
	Addrs    []string
	Err      string
	LastSeen time.Time
}

// Start starts a server. It listens on the addresses it can and keeps
// looking for the ones it can't (Tailscale may come up later).
func Start(o Options) *Server {
	s := newServer(o)
	s.relisten()
	go s.watchAddrs()
	return s
}

// newServer makes a server that doesn't listen yet.
func newServer(o Options) *Server {
	if o.Port == 0 {
		o.Port = DefaultPort
	}
	s := &Server{o: o, ev: newEvents(), stop: make(chan struct{}), token: o.Token,
		lns: map[string]net.Listener{}, sent: map[string]Message{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/ping", s.auth(s.ping))
	mux.HandleFunc("GET /v1/chats", s.auth(s.chats))
	mux.HandleFunc("GET /v1/chats/{chat}/messages", s.auth(s.messages))
	mux.HandleFunc("GET /v1/chats/{chat}/messages/{id}", s.auth(s.message))
	mux.HandleFunc("POST /v1/chats/{chat}/messages", s.auth(s.send))
	mux.HandleFunc("POST /v1/chats/{chat}/messages/{id}/react", s.auth(s.react))
	mux.HandleFunc("POST /v1/chats/{chat}/read", s.auth(s.read))
	mux.HandleFunc("GET /v1/events", s.auth(s.events))
	mux.HandleFunc("GET /v1/search", s.auth(s.search))
	mux.HandleFunc("GET /v1/chats/{chat}/photo/version", s.auth(s.photoVersion))
	mux.HandleFunc("GET /v1/chats/{chat}/photo", s.auth(s.photo))
	mux.HandleFunc("GET /v1/chats/{chat}/messages/{id}/image", s.auth(s.picture))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fail(w, http.StatusNotFound, "no such endpoint") })
	s.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      maxWait + 15*time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	return s
}

// Close stops the server and drops its connections.
func (s *Server) Close() {
	close(s.stop)
	s.srv.Close()
	s.mu.Lock()
	clear(s.lns)
	s.mu.Unlock()
}

// SetToken changes the token: requests with the old one fail from now on.
func (s *Server) SetToken(t string) {
	s.mu.Lock()
	s.token = t
	s.mu.Unlock()
}

// Status reports where the server listens and when a watch last asked.
func (s *Server) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{Err: s.err, LastSeen: s.lastSeen}
	for a := range s.lns {
		st.Addrs = append(st.Addrs, a)
	}
	return st
}

// Event passes on a backend event. Call it on the UI goroutine.
func (s *Server) Event(e model.Event) {
	if w, ok := event(e); ok {
		s.ev.add(w)
	}
	s.wake(e)
}

// Reset tells watches to reload everything: another account opened.
func (s *Server) Reset() {
	s.ev.add(Event{Type: "reset"})
	clear(s.sent)
	s.sentOrder = nil
}

// Addrs are the addresses a server listens on: every network's, or this
// computer's own (for testing it) and its Tailscale addresses.
func Addrs(port int, all bool) []string {
	p := strconv.Itoa(port)
	if all {
		return []string{":" + p}
	}
	addrs := []string{net.JoinHostPort("127.0.0.1", p)}
	for _, ip := range TailscaleIPs() {
		addrs = append(addrs, net.JoinHostPort(ip.String(), p))
	}
	return addrs
}

var (
	tailnet4 = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}
	tailnet6 = &net.IPNet{IP: net.ParseIP("fd7a:115c:a1e0::"), Mask: net.CIDRMask(48, 128)}
)

// TailscaleIPs are this computer's addresses on its tailnet, IPv4 first.
func TailscaleIPs() []net.IP {
	ifAddrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var v4, v6 []net.IP
	for _, a := range ifAddrs {
		n, ok := a.(*net.IPNet)
		switch {
		case !ok:
		case tailnet4.Contains(n.IP):
			v4 = append(v4, n.IP.To4())
		case tailnet6.Contains(n.IP):
			v6 = append(v6, n.IP)
		}
	}
	return append(v4, v6...)
}

// watchAddrs listens on new addresses as they come and lets go of the ones
// that went.
func (s *Server) watchAddrs() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.relisten()
		}
	}
}

func (s *Server) relisten() {
	want := Addrs(s.o.Port, s.o.AllNetworks)
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.stop:
		return
	default:
	}
	for a, ln := range s.lns {
		if !slices.Contains(want, a) {
			ln.Close()
			delete(s.lns, a)
		}
	}
	s.err = ""
	for _, a := range want {
		if s.lns[a] != nil {
			continue
		}
		ln, err := net.Listen("tcp", a)
		if err != nil {
			s.err = err.Error()
			continue
		}
		s.lns[a] = ln
		go func() {
			_ = s.srv.Serve(ln)
			s.mu.Lock()
			if s.lns[a] == ln { // it failed: the next look listens again
				delete(s.lns, a)
			}
			s.mu.Unlock()
		}()
	}
}

// auth lets requests with the token through.
func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		s.mu.Lock()
		tok := s.token
		ok = ok && tok != "" && subtle.ConstantTimeCompare([]byte(got), []byte(tok)) == 1
		if ok {
			s.lastSeen = time.Now()
		}
		s.mu.Unlock()
		if !ok {
			fail(w, http.StatusUnauthorized, "wrong token")
			return
		}
		h(w, r)
	}
}

// errBusy is a request the UI goroutine didn't get to in time.
var errBusy = errors.New("the app is busy, try again")

// apiError is a request that can't be answered.
type apiError struct {
	code int
	msg  string
}

// call runs f on the UI goroutine and writes what it returns.
func (s *Server) call(w http.ResponseWriter, code int, f func() (any, *apiError)) {
	var v any
	var e *apiError
	if err := s.o.Do(func() { v, e = f() }); err != nil {
		fail(w, http.StatusServiceUnavailable, errBusy.Error())
		return
	}
	if e != nil {
		fail(w, e.code, e.msg)
		return
	}
	reply(w, code, v)
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	reply(w, code, map[string]string{"error": msg})
}

// intParam reads a query parameter from 0 to max, or def when it's missing.
func intParam(r *http.Request, name string, def, max int) (int, bool) {
	s := r.URL.Query().Get(name)
	if s == "" {
		return def, true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > max {
		return 0, false
	}
	return n, true
}

func (s *Server) ping(w http.ResponseWriter, r *http.Request) {
	s.call(w, http.StatusOK, func() (any, *apiError) {
		c := s.o.Host.Conn()
		return map[string]string{
			"version": s.o.Version,
			"state":   stateName(c.State),
			"me":      c.Me,
			"cursor":  s.ev.cursor(),
		}, nil
	})
}

func (s *Server) chats(w http.ResponseWriter, r *http.Request) {
	limit, ok := intParam(r, "limit", 20, 50)
	if !ok || limit == 0 {
		fail(w, http.StatusBadRequest, "limit must be from 1 to 50")
		return
	}
	after := r.URL.Query().Get("after")
	s.call(w, http.StatusOK, func() (any, *apiError) {
		// Events come on this goroutine too, so the cursor matches the list.
		cursor := s.ev.cursor()
		list := listed(s.o.Host.Backend().Chats())
		if after != "" {
			i := 0
			for i < len(list) && list[i].ID != after {
				i++
			}
			if i == len(list) {
				return nil, &apiError{http.StatusNotFound, "no such chat in the list"}
			}
			list = list[i+1:]
		}
		more := len(list) > limit
		if more {
			list = list[:limit]
		}
		out := make([]Chat, len(list))
		for i, c := range list {
			out[i] = toChat(c)
		}
		return map[string]any{"chats": out, "more": more, "cursor": cursor}, nil
	})
}

func (s *Server) messages(w http.ResponseWriter, r *http.Request) {
	limit, ok := intParam(r, "limit", 15, 50)
	if !ok || limit == 0 {
		fail(w, http.StatusBadRequest, "limit must be from 1 to 50")
		return
	}
	chat, before := r.PathValue("chat"), r.URL.Query().Get("before")
	s.call(w, http.StatusOK, func() (any, *apiError) {
		b := s.o.Host.Backend()
		var msgs []*model.Message
		if before == "" {
			msgs = b.Messages(chat, limit+1)
		} else {
			msgs = b.MessagesBefore(chat, before, limit+1)
		}
		more := len(msgs) > limit
		if more {
			msgs = msgs[len(msgs)-limit:]
		}
		out := make([]Message, len(msgs))
		for i, m := range msgs {
			out[i] = toMessage(m, textLimit)
		}
		return map[string]any{"messages": out, "more": more}, nil
	})
}

func (s *Server) message(w http.ResponseWriter, r *http.Request) {
	chat, id := r.PathValue("chat"), r.PathValue("id")
	s.call(w, http.StatusOK, func() (any, *apiError) {
		msgs := s.o.Host.Backend().MessagesFrom(chat, id, 1)
		if len(msgs) == 0 || msgs[0].ID != id {
			return nil, &apiError{http.StatusNotFound, "no such message"}
		}
		return map[string]any{"message": toMessage(msgs[0], -1)}, nil
	})
}

// sendKeep is how many nonces the server remembers.
const sendKeep = 64

func (s *Server) send(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text    string `json:"text"`
		ReplyTo string `json:"replyTo"`
		Nonce   string `json:"nonce"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "the body must be JSON: {\"text\": ...}")
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" || !utf8.ValidString(text) {
		fail(w, http.StatusBadRequest, "text is empty")
		return
	}
	if utf8.RuneCountInString(text) > sendLimit {
		fail(w, http.StatusBadRequest, "text is longer than "+strconv.Itoa(sendLimit)+" characters")
		return
	}
	chat := r.PathValue("chat")
	key := chat + "\x00" + req.Nonce
	s.call(w, http.StatusCreated, func() (any, *apiError) {
		if req.Nonce != "" {
			if m, ok := s.sent[key]; ok {
				return map[string]any{"message": m}, nil
			}
		}
		if !s.o.Host.Conn().State.LoggedIn() {
			return nil, &apiError{http.StatusServiceUnavailable, "WhatsApp isn't linked"}
		}
		b := s.o.Host.Backend()
		c := findChat(b.Chats(), chat)
		if c == nil {
			return nil, &apiError{http.StatusNotFound, "no such chat"}
		}
		if c.Left {
			return nil, &apiError{http.StatusConflict, "you're no longer in this group"}
		}
		d := model.Draft{Text: text}
		if req.ReplyTo != "" {
			if ms := b.MessagesFrom(chat, req.ReplyTo, 1); len(ms) == 1 && ms[0].ID == req.ReplyTo {
				d.Reply = ms[0]
			}
		}
		m := b.Send(chat, d)
		if m == nil {
			return nil, &apiError{http.StatusServiceUnavailable, "WhatsApp isn't connected"}
		}
		s.o.Host.Sent(m)
		s.o.Host.Read(chat) // answering a chat reads it, as on the phone
		wm := toMessage(m, textLimit)
		if req.Nonce != "" {
			s.remember(key, wm)
		}
		return map[string]any{"message": wm}, nil
	})
}

func (s *Server) remember(key string, m Message) {
	if len(s.sentOrder) == sendKeep {
		delete(s.sent, s.sentOrder[0])
		s.sentOrder = s.sentOrder[1:]
	}
	s.sent[key] = m
	s.sentOrder = append(s.sentOrder, key)
}

func findChat(chats []*model.Chat, id string) *model.Chat {
	for _, c := range chats {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// react sets your reaction to a message, or takes it back. The message
// comes again in a "message" event, as on every change.
func (s *Server) react(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Emoji string `json:"emoji"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "the body must be JSON: {\"emoji\": ...}")
		return
	}
	// An emoji is a few code points at most (a flag, skin tones, ZWJ sequences).
	if !utf8.ValidString(req.Emoji) || utf8.RuneCountInString(req.Emoji) > 16 || strings.TrimSpace(req.Emoji) != req.Emoji {
		fail(w, http.StatusBadRequest, "emoji must be one emoji, or empty")
		return
	}
	chat, id := r.PathValue("chat"), r.PathValue("id")
	s.call(w, http.StatusOK, func() (any, *apiError) {
		if !s.o.Host.Conn().State.LoggedIn() {
			return nil, &apiError{http.StatusServiceUnavailable, "WhatsApp isn't linked"}
		}
		b := s.o.Host.Backend()
		ms := b.MessagesFrom(chat, id, 1)
		if len(ms) == 0 || ms[0].ID != id {
			return nil, &apiError{http.StatusNotFound, "no such message"}
		}
		m := ms[0]
		if m.Kind == model.KindDeleted || m.Kind == model.KindSystem {
			return nil, &apiError{http.StatusConflict, "this message can't take a reaction"}
		}
		if c := findChat(b.Chats(), chat); c != nil && c.Left {
			return nil, &apiError{http.StatusConflict, "you're no longer in this group"}
		}
		b.React(m, req.Emoji)
		return map[string]any{}, nil
	})
}

func (s *Server) read(w http.ResponseWriter, r *http.Request) {
	chat := r.PathValue("chat")
	s.call(w, http.StatusOK, func() (any, *apiError) {
		s.o.Host.Read(chat)
		return map[string]any{}, nil
	})
}

// maxWait is the longest an events request waits for one.
const maxWait = 30 * time.Second

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	wait, ok := intParam(r, "wait", 25, int(maxWait/time.Second))
	if !ok {
		fail(w, http.StatusBadRequest, "wait must be from 0 to 30 seconds")
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		select {
		case <-s.stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	evs, cursor, reset := s.ev.since(ctx, r.URL.Query().Get("since"), time.Duration(wait)*time.Second)
	if evs == nil {
		evs = []Event{}
	}
	reply(w, http.StatusOK, map[string]any{"events": evs, "cursor": cursor, "reset": reset})
}
