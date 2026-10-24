package webtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/meatlover/modelpick/internal/web"
)

const (
	port  = 4455
	token = "webtest-token"
	host  = "127.0.0.1:4455"
)

// client talks to the real handler around a back end, so what is checked
// is the response body the page receives.
type client struct {
	t      *testing.T
	h      http.Handler
	cookie *http.Cookie
}

func newClient(t *testing.T, b web.Backend) *client {
	t.Helper()
	c := &client{t: t, h: web.NewServer(b, port, token)}
	r := httptest.NewRequest(http.MethodGet, "http://"+host+"/?t="+token, nil)
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	cookies := w.Result().Cookies()
	if w.Code != http.StatusSeeOther || len(cookies) != 1 {
		t.Fatalf("login: %d, %d cookies", w.Code, len(cookies))
	}
	c.cookie = cookies[0]
	return c
}

func (c *client) do(method, path, body string) string {
	c.t.Helper()
	return c.send(method, path, body).Body.String()
}

// send is do with the whole response, for a case that checks the status
// or a header.
func (c *client) send(method, path, body string) *httptest.ResponseRecorder {
	c.t.Helper()
	r := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	r.AddCookie(c.cookie)
	if method != http.MethodGet {
		r.Header.Set("Origin", "http://"+host)
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	if w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden {
		c.t.Fatalf("%s %s: %d", method, path, w.Code)
	}
	return w
}

func (c *client) state() web.State {
	c.t.Helper()
	var s web.State
	if err := json.Unmarshal([]byte(c.do(http.MethodGet, "/api/state", "")), &s); err != nil {
		c.t.Fatal(err)
	}
	return s
}

// bodies is every body a page session can see: the three reads, one call
// of each operation, a refresh of each account, a Check all, and the reads
// again while the config is broken.
func bodies(t *testing.T, h Harness, b web.Backend) []string {
	t.Helper()
	c := newClient(t, b)
	reads := func() []string {
		return []string{c.do("GET", "/api/state", ""), c.do("GET", "/api/usage", ""), c.do("GET", "/api/doctor", "")}
	}
	out := reads()
	s := c.state()
	for _, a := range s.Accounts {
		out = append(out, c.do("POST", "/api/usage/"+url.PathEscape(a.Name)+"/refresh", ""))
	}
	for _, o := range everyOp(t, s) {
		req, err := json.Marshal(map[string]any{"base": c.state().SHA256, "op": o.op, "args": o.args})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, c.do("POST", "/api/op", string(req)))
	}
	out = append(out, c.do("POST", "/api/doctor/all", ""))
	out = append(out, reads()...)
	h.Break(t)
	out = append(out, reads()...)
	h.Fix(t)
	return out
}

func noSecrets(t *testing.T, h Harness) {
	if len(h.Secrets) == 0 {
		t.Fatal("the harness names no secret to look for")
	}
	b := h.New(t)
	for _, body := range bodies(t, h, b) {
		for _, s := range h.Secrets {
			if s != "" && strings.Contains(body, s) {
				t.Errorf("a response body contains a secret: %.200s", body)
			}
		}
	}
}
