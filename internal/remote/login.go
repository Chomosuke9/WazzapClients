package remote

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/chomosuke9/wazzapclients/internal/model"
	"github.com/chomosuke9/wazzapclients/internal/termqr"
)

// Linking the account over HTTP, for the headless mode (cmd/wazzap
// -headless), where no window shows the QR code: /login is a page that
// asks for the token and then shows the QR code, or a pairing code for a
// phone number, through the /v1/login endpoints.

//go:embed login.html
var loginHTML []byte

// pairing lets one pairing code request run at a time: each asks
// WhatsApp's server, which answers a flood with errors.
var pairing sync.Mutex

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src blob:; connect-src 'self'; form-action 'none'; frame-ancestors 'none'")
	h.Set("Referrer-Policy", "no-referrer")
	_, _ = w.Write(loginHTML)
}

// loginStep is where linking is: "starting", "qr" (a code to scan),
// "expired" (the codes ran out: retry), "linked" or "error".
func loginStep(c model.ConnEvent) string {
	switch {
	case c.State.LoggedIn():
		return "linked"
	case c.State == model.StateQR && c.QR != "":
		return "qr"
	case c.State == model.StateQRExpired:
		return "expired"
	case c.State == model.StateError:
		return "error"
	}
	return "starting"
}

func (s *Server) loginState(w http.ResponseWriter, r *http.Request) {
	s.call(w, http.StatusOK, func() (any, *apiError) {
		c := s.o.Host.Conn()
		_, phone := s.o.Host.Backend().(model.PhonePairer)
		return map[string]any{
			"step":  loginStep(c),
			"state": stateName(c.State),
			"me":    c.Me,
			"error": c.Err,
			// The code changes every 20 seconds or so: the page fetches
			// the picture again when this does.
			"qr":    qrID(c.QR),
			"phone": phone, // linking with a phone number works
		}, nil
	})
}

// qrID names a QR code without giving it away to a page without the
// token: the code itself links the account.
func qrID(code string) string {
	if code == "" {
		return ""
	}
	var h uint32 = 2166136261
	for i := 0; i < len(code); i++ {
		h = (h ^ uint32(code[i])) * 16777619
	}
	const hex = "0123456789abcdef"
	b := make([]byte, 8)
	for i := range b {
		b[i] = hex[h>>(28-4*i)&15]
	}
	return string(b)
}

func (s *Server) loginQR(w http.ResponseWriter, r *http.Request) {
	var code string
	if err := s.o.Do(func() {
		if c := s.o.Host.Conn(); loginStep(c) == "qr" {
			code = c.QR
		}
	}); err != nil {
		fail(w, http.StatusServiceUnavailable, errBusy.Error())
		return
	}
	if code == "" {
		fail(w, http.StatusNotFound, "there's no QR code to scan now")
		return
	}
	svg, err := termqr.SVG(code)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(svg)
}

func (s *Server) loginCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "the body must be JSON: {\"phone\": ...}")
		return
	}
	phone := digits(req.Phone)
	if len(phone) < 7 || len(phone) > 15 || phone[0] == '0' {
		fail(w, http.StatusBadRequest, "the phone number must be in international format, with its country code: 628123456789")
		return
	}
	var p model.PhonePairer
	var e *apiError
	if err := s.o.Do(func() {
		if s.o.Host.Conn().State.LoggedIn() {
			e = &apiError{http.StatusConflict, "WhatsApp is linked already"}
			return
		}
		var ok bool
		if p, ok = s.o.Host.Backend().(model.PhonePairer); !ok {
			e = &apiError{http.StatusNotImplemented, "this backend can't link with a phone number"}
		}
	}); err != nil {
		fail(w, http.StatusServiceUnavailable, errBusy.Error())
		return
	}
	if e != nil {
		fail(w, e.code, e.msg)
		return
	}
	if !pairing.TryLock() {
		fail(w, http.StatusTooManyRequests, "a pairing code is on its way already")
		return
	}
	defer pairing.Unlock()
	// Off the UI goroutine: it waits for WhatsApp's server.
	code, err := p.PairPhone(phone)
	if err != nil {
		fail(w, http.StatusBadGateway, "WhatsApp didn't give a code: "+err.Error())
		return
	}
	reply(w, http.StatusOK, map[string]string{"code": code})
}

func (s *Server) loginRetry(w http.ResponseWriter, r *http.Request) {
	s.call(w, http.StatusOK, func() (any, *apiError) {
		if s.o.Host.Conn().State.LoggedIn() {
			return nil, &apiError{http.StatusConflict, "WhatsApp is linked already"}
		}
		s.o.Host.Backend().Retry()
		return map[string]any{}, nil
	})
}

// digits keeps a phone number's digits: "+62 812-3456" is 628123456.
func digits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}
