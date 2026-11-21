package remote

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/chomosuke9/wazzapclients/internal/model"
)

func TestLogin(t *testing.T) {
	s, h := newTest(t)
	// The page itself needs no token; everything it asks for does.
	r := httptest.NewRequest("GET", "/login", nil)
	w := httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "/v1/login") {
		t.Fatalf("/login: %d", w.Code)
	}
	r = httptest.NewRequest("GET", "/v1/login", nil)
	w = httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("/v1/login without the token: %d", w.Code)
	}

	var st map[string]any
	if code := do(t, s, "GET", "/v1/login", "", &st); code != http.StatusOK || st["step"] != "linked" {
		t.Fatalf("linked: %d %v", code, st)
	}
	if code := do(t, s, "POST", "/v1/login/code", `{"phone":"+62 812-3456-7890"}`, nil); code != http.StatusConflict {
		t.Fatalf("a code while linked: %d", code)
	}

	h.conn = model.ConnEvent{State: model.StateQR, QR: "2@abc,def,ghi"}
	if code := do(t, s, "GET", "/v1/login", "", &st); code != http.StatusOK || st["step"] != "qr" || st["qr"] == "" || st["phone"] != true {
		t.Fatalf("showing a QR code: %d %v", code, st)
	}
	if strings.Contains(st["qr"].(string), "abc") {
		t.Fatal("the state gives the QR code away")
	}
	r = httptest.NewRequest("GET", "/v1/login/qr.svg", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	w = httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "<svg") {
		t.Fatalf("qr.svg: %d %.40q", w.Code, w.Body)
	}
	var got map[string]string
	if code := do(t, s, "POST", "/v1/login/code", `{"phone":"+62 812-3456-7890"}`, &got); code != http.StatusOK || got["code"] != "WAZZ-DEMO" {
		t.Fatalf("a pairing code: %d %v", code, got)
	}
	for _, bad := range []string{`{"phone":"08123456789"}`, `{"phone":"12"}`, `nope`} {
		if code := do(t, s, "POST", "/v1/login/code", bad, nil); code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", bad, code)
		}
	}

	h.conn = model.ConnEvent{State: model.StateQRExpired}
	if do(t, s, "GET", "/v1/login", "", &st); st["step"] != "expired" {
		t.Fatalf("expired: %v", st)
	}
	r = httptest.NewRequest("GET", "/v1/login/qr.svg", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	w = httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("qr.svg without a code: %d", w.Code)
	}
	if code := do(t, s, "POST", "/v1/login/retry", "", nil); code != http.StatusOK {
		t.Fatalf("retry: %d", code)
	}
}

func TestListenAddrs(t *testing.T) {
	got := ListenAddrs([]string{"localhost", "0.0.0.0", "::1", "[fd00::1]", "all", "localhost"}, 9000)
	want := []string{":9000", "0.0.0.0:9000", "127.0.0.1:9000", "[::1]:9000", "[fd00::1]:9000"}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
