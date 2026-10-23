package web

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// captureLog sends the server's error log to a buffer for one test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	errorLog.SetOutput(&buf)
	t.Cleanup(func() { errorLog.SetOutput(os.Stderr) })
	return &buf
}

// refreshStub fails Refresh with err.
type refreshStub struct {
	stub
	err error
}

func (s *refreshStub) Refresh(string) (UsageRow, error) { return UsageRow{}, s.err }

// Final review Important 5: an unexpected error's text never reaches the
// page; it goes to stderr.
func TestInternalErrorRedacted(t *testing.T) {
	logged := captureLog(t)
	secret := errors.New("open /home/u/.config/modelpick/config.json: sk-detail")
	w := do(t, newTestServer(&stub{applyErr: secret}), "POST", "/api/op", goodHost, "http://127.0.0.1:4455", true)
	if w.Code != 500 || strings.TrimSpace(w.Body.String()) != `{"reason":"internal error"}` {
		t.Errorf("op: got %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(logged.String(), "sk-detail") {
		t.Errorf("op error not logged: %q", logged.String())
	}
}

func TestRefreshErrorRedacted(t *testing.T) {
	logged := captureLog(t)
	h := NewServer(&refreshStub{err: errors.New("dial tcp: sk-detail")}, testPort, testToken)
	w := do(t, h, "POST", "/api/usage/go-1/refresh", goodHost, "http://127.0.0.1:4455", true)
	if w.Code != 422 || strings.TrimSpace(w.Body.String()) != `{"reason":"go-1: refresh failed"}` {
		t.Errorf("refresh: got %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(logged.String(), "sk-detail") {
		t.Errorf("refresh error not logged: %q", logged.String())
	}
	h = NewServer(&refreshStub{err: &Refused{Reason: "go-1: expired token"}}, testPort, testToken)
	w = do(t, h, "POST", "/api/usage/go-1/refresh", goodHost, "http://127.0.0.1:4455", true)
	if w.Code != 422 || strings.TrimSpace(w.Body.String()) != `{"reason":"go-1: expired token"}` {
		t.Errorf("refused refresh: got %d %s", w.Code, w.Body.String())
	}
}

// The single-instance check (spec §8.1) has the token but no cookie.
func TestPingWithToken(t *testing.T) {
	h := newTestServer(&stub{})
	if w := do(t, h, "GET", "/api/ping?t="+testToken, goodHost, "", false); w.Code != 204 {
		t.Errorf("ping with token: %d", w.Code)
	}
	if w := do(t, h, "GET", "/api/ping?t=wrong", goodHost, "", false); w.Code != 401 {
		t.Errorf("ping with a wrong token: %d", w.Code)
	}
	if w := do(t, h, "GET", "/api/state?t="+testToken, goodHost, "", false); w.Code != 401 {
		t.Errorf("state with a token but no cookie: %d", w.Code)
	}
	if w := do(t, h, "GET", "/api/ping?t="+testToken, "evil.example:4455", "", false); w.Code != 403 {
		t.Errorf("ping with token from a foreign host: %d", w.Code)
	}
}

// Two servers on different ports must not share a cookie on 127.0.0.1.
func TestCookiePerPort(t *testing.T) {
	h := NewServer(&stub{}, 5566, testToken)
	r := httptest.NewRequest("GET", "http://127.0.0.1:5566/?t="+testToken, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if c := w.Header().Get("Set-Cookie"); !strings.HasPrefix(c, "modelpick_ui_5566="+testToken+";") {
		t.Errorf("cookie %q, want modelpick_ui_5566", c)
	}
	r = httptest.NewRequest("GET", "http://127.0.0.1:5566/api/state", nil)
	r.AddCookie(&http.Cookie{Name: "modelpick_ui_4455", Value: testToken})
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Errorf("another port's cookie: %d, want 401", w.Code)
	}
}

func TestFrameAncestors(t *testing.T) {
	w := do(t, newTestServer(&stub{}), "GET", "/", goodHost, "", true)
	if got := w.Header().Get("Content-Security-Policy"); got != "default-src 'self'; frame-ancestors 'none'" {
		t.Errorf("CSP %q", got)
	}
}
