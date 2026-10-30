package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testPort = 4455
const testToken = "tok123"

type stub struct {
	applyErr error
	applyRes OpResult
	replaced bool
}

func (s *stub) State() State {
	return State{SHA256: "abc", Accounts: []Account{}, BinaryReplaced: s.replaced}
}
func (s *stub) Usage() []UsageRow {
	return []UsageRow{{Account: "a"}}
}
func (s *stub) Refresh(a string) (UsageRow, error) { return UsageRow{Account: a}, nil }
func (s *stub) Doctor() Doctor                     { return Doctor{Findings: []Finding{}} }
func (s *stub) DoctorAll() Doctor                  { return Doctor{Findings: []Finding{}} }
func (s *stub) Apply(OpRequest) (OpResult, error)  { return s.applyRes, s.applyErr }

func do(t *testing.T, h http.Handler, method, path, host, origin string, authed bool) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(`{"base":"abc","op":"x"}`))
	r.Host = host
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if authed {
		r.AddCookie(&http.Cookie{Name: cookieName(testPort), Value: testToken})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

const goodHost = "127.0.0.1:4455"

func newTestServer(s *stub) http.Handler { return NewServer(s, testPort, testToken) }

func TestUnauthenticatedIs401(t *testing.T) {
	h := newTestServer(&stub{})
	for _, p := range []string{"/", "/api/state", "/js/main.js"} {
		if w := do(t, h, "GET", p, goodHost, "", false); w.Code != 401 {
			t.Errorf("%s: got %d want 401", p, w.Code)
		}
	}
}

func TestTokenRedirectSetsCookie(t *testing.T) {
	h := newTestServer(&stub{})
	w := do(t, h, "GET", "/?t="+testToken, goodHost, "", false)
	if w.Code != 303 || w.Header().Get("Location") != "/" {
		t.Fatalf("got %d loc %q", w.Code, w.Header().Get("Location"))
	}
	c := w.Header().Get("Set-Cookie")
	for _, want := range []string{"modelpick_ui_4455=" + testToken, "HttpOnly", "SameSite=Strict", "Path=/"} {
		if !strings.Contains(c, want) {
			t.Errorf("cookie %q lacks %q", c, want)
		}
	}
	if w := do(t, h, "GET", "/?t=wrong", goodHost, "", false); w.Code != 401 {
		t.Errorf("wrong token: got %d", w.Code)
	}
	if w := do(t, h, "GET", "/", goodHost, "", true); w.Code != 200 {
		t.Errorf("cookie only: got %d", w.Code)
	}
}

func TestHostCheck(t *testing.T) {
	h := newTestServer(&stub{})
	if w := do(t, h, "GET", "/api/ping", "evil.example:4455", "", true); w.Code != 403 {
		t.Errorf("evil host: got %d", w.Code)
	}
	for _, host := range []string{"localhost:4455", "127.0.0.1:4455"} {
		if w := do(t, h, "GET", "/api/ping", host, "", true); w.Code != 204 {
			t.Errorf("%s: got %d", host, w.Code)
		}
	}
}

func TestOriginCheck(t *testing.T) {
	h := newTestServer(&stub{})
	if w := do(t, h, "POST", "/api/op", goodHost, "http://evil.example", true); w.Code != 403 {
		t.Errorf("evil origin: got %d", w.Code)
	}
	for _, o := range []string{"http://127.0.0.1:4455", "http://localhost:4455"} {
		if w := do(t, h, "POST", "/api/op", goodHost, o, true); w.Code != 200 {
			t.Errorf("origin %s: got %d", o, w.Code)
		}
	}
}

func TestHeadersOnEveryResponse(t *testing.T) {
	h := newTestServer(&stub{})
	cases := []*httptest.ResponseRecorder{
		do(t, h, "GET", "/", goodHost, "", false),
		do(t, h, "GET", "/api/ping", "evil.example:4455", "", true),
		do(t, h, "POST", "/api/op", goodHost, "http://evil.example", true),
		do(t, h, "GET", "/nope", goodHost, "", true),
		do(t, h, "GET", "/api/state", goodHost, "", true),
	}
	for i, w := range cases {
		if got := w.Header().Get("Content-Security-Policy"); got != "default-src 'self'; frame-ancestors 'none'" {
			t.Errorf("case %d (%d): CSP %q", i, w.Code, got)
		}
		for k := range w.Header() {
			if strings.HasPrefix(k, "Access-Control-") {
				t.Errorf("case %d: has %s", i, k)
			}
		}
	}
	if cases[3].Code != 404 {
		t.Errorf("unknown path: got %d", cases[3].Code)
	}
}

func TestOpResponses(t *testing.T) {
	cases := []struct {
		name string
		s    *stub
		code int
		body string
	}{
		{"conflict", &stub{applyErr: Conflict{SHA256: "new"}}, 409, `{"sha256":"new"}`},
		{"refused", &stub{applyErr: Refused{Reason: "x", Uses: 2}}, 422, `{"reason":"x","uses":2}`},
		{"refused no uses", &stub{applyErr: Refused{Reason: "x"}}, 422, `{"reason":"x"}`},
		{"conflict pointer", &stub{applyErr: &Conflict{SHA256: "new"}}, 409, `{"sha256":"new"}`},
		{"refused pointer", &stub{applyErr: &Refused{Reason: "x", Uses: 2}}, 422, `{"reason":"x","uses":2}`},
		{"refused wrapped pointer", &stub{applyErr: fmt.Errorf("op: %w", &Refused{Reason: "y"})}, 422, `{"reason":"y"}`},
		{"ok", &stub{applyRes: OpResult{SHA256: "s", Findings: []Finding{}}}, 200, `{"sha256":"s","findings":[]}`},
	}
	for _, c := range cases {
		w := do(t, newTestServer(c.s), "POST", "/api/op", goodHost, "http://127.0.0.1:4455", true)
		if w.Code != c.code || strings.TrimSpace(w.Body.String()) != c.body {
			t.Errorf("%s: got %d %q want %d %q", c.name, w.Code, w.Body.String(), c.code, c.body)
		}
	}
}

func TestPingAndRefreshMethods(t *testing.T) {
	h := newTestServer(&stub{})
	if w := do(t, h, "GET", "/api/ping", goodHost, "", true); w.Code != 204 {
		t.Errorf("ping: %d", w.Code)
	}
	if w := do(t, h, "GET", "/api/usage/a/refresh", goodHost, "", true); w.Code != 405 {
		t.Errorf("GET refresh: %d", w.Code)
	}
	w := do(t, h, "POST", "/api/usage/a/refresh", goodHost, "http://localhost:4455", true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"account":"a"`) {
		t.Errorf("POST refresh: %d %s", w.Code, w.Body.String())
	}
}

func TestStaticContentTypes(t *testing.T) {
	h := newTestServer(&stub{})
	for path, want := range map[string]string{
		"/css/tokens.css": "text/css; charset=utf-8",
		"/js/main.js":     "text/javascript",
		"/":               "text/html",
	} {
		w := do(t, h, "GET", path, goodHost, "", true)
		if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), want) {
			t.Errorf("%s: %d %q", path, w.Code, w.Header().Get("Content-Type"))
		}
	}
}

func TestCapEncoding(t *testing.T) {
	enc := func(c Choice) string {
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if s := enc(Choice{}); !strings.Contains(s, `"cap":null`) {
		t.Errorf("nil cap: %s", s)
	}
	if s := enc(Choice{Cap: []any{}}); !strings.Contains(s, `"cap":[]`) {
		t.Errorf("empty cap: %s", s)
	}
	if s := enc(Choice{Cap: []any{90.0, "pace+10"}}); !strings.Contains(s, `"cap":[90,"pace+10"]`) {
		t.Errorf("mixed cap: %s", s)
	}
}

func TestNewToken(t *testing.T) {
	a, err := NewToken()
	b, _ := NewToken()
	if err != nil || len(a) != 64 || a == b {
		t.Errorf("token %q %q %v", a, b, err)
	}
}

// A known route asked with the wrong method says so, with Allow, instead of
// falling through to the file server's 404.
func TestWrongMethodIs405(t *testing.T) {
	h := newTestServer(&stub{})
	cases := []struct{ method, path, allow string }{
		{"POST", "/api/state", "GET, HEAD"},
		{"POST", "/api/usage", "GET, HEAD"},
		{"POST", "/api/doctor", "GET, HEAD"},
		{"POST", "/api/ping", "GET, HEAD"},
		{"GET", "/api/op", "POST"},
		{"GET", "/api/doctor/all", "POST"},
		{"DELETE", "/api/usage/a/refresh", "POST"},
	}
	for _, c := range cases {
		w := do(t, h, c.method, c.path, goodHost, "http://127.0.0.1:4455", true)
		if w.Code != 405 || w.Header().Get("Allow") != c.allow {
			t.Errorf("%s %s: got %d Allow %q, want 405 Allow %q", c.method, c.path, w.Code, w.Header().Get("Allow"), c.allow)
		}
	}
	if w := do(t, h, "HEAD", "/api/state", goodHost, "", true); w.Code != 200 {
		t.Errorf("HEAD /api/state: got %d, want 200", w.Code)
	}
}

func TestOpBodyErrors(t *testing.T) {
	h := newTestServer(&stub{})
	post := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://"+goodHost+"/api/op", strings.NewReader(body))
		r.Host = goodHost
		r.Header.Set("Origin", "http://127.0.0.1:4455")
		r.AddCookie(&http.Cookie{Name: cookieName(testPort), Value: testToken})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := post(`{"base":`); w.Code != 400 || strings.TrimSpace(w.Body.String()) != `{"reason":"bad request body"}` {
		t.Errorf("bad JSON: got %d %s", w.Code, w.Body.String())
	}
	big := `{"base":"abc","op":"x","args":{"pad":"` + strings.Repeat("a", 1<<20) + `"}}`
	if w := post(big); w.Code != 413 || strings.TrimSpace(w.Body.String()) != `{"reason":"request body too large"}` {
		t.Errorf("over 1 MiB: got %d %s", w.Code, w.Body.String())
	}
}

func TestAuthEdges(t *testing.T) {
	h := newTestServer(&stub{})
	req := func(method, target, host, origin, cookie string) int {
		r := httptest.NewRequest(method, "http://"+goodHost+target, strings.NewReader(`{"base":"abc","op":"x"}`))
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: cookieName(testPort), Value: cookie})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	const origin = "http://127.0.0.1:4455"
	cases := []struct {
		name                         string
		method, target, host, origin string
		cookie                       string
		want                         int
	}{
		{"wrong cookie", "GET", "/api/state", goodHost, "", "wrong", 401},
		{"wrong cookie, wrong token on /", "GET", "/?t=wrong", goodHost, "", "wrong", 401},
		{"wrong cookie, right token on / logs in", "GET", "/?t=" + testToken, goodHost, "", "wrong", 303},
		{"token on a static file", "GET", "/js/main.js?t=" + testToken, goodHost, "", "", 401},
		{"token on a POST to /", "POST", "/?t=" + testToken, goodHost, origin, "", 401},
		{"POST without Origin", "POST", "/api/op", goodHost, "", testToken, 403},
		{"Host without port", "GET", "/api/state", "127.0.0.1", "", testToken, 403},
		{"Host on another port", "GET", "/api/state", "127.0.0.1:4456", "", testToken, 403},
		{"Origin on another port", "POST", "/api/op", goodHost, "http://127.0.0.1:4456", testToken, 403},
		{"HEAD without Origin", "HEAD", "/api/state", goodHost, "", testToken, 200},
	}
	for _, c := range cases {
		if got := req(c.method, c.target, c.host, c.origin, c.cookie); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestStateCarriesBinaryReplaced(t *testing.T) {
	for _, want := range []bool{false, true} {
		w := do(t, newTestServer(&stub{replaced: want}), "GET", "/api/state", goodHost, "", true)
		var got map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("%v: %s", err, w.Body.String())
		}
		v, ok := got["binary_replaced"]
		if !ok || v != want {
			t.Errorf("want binary_replaced %v, got %v (present %v) in %s", want, v, ok, w.Body.String())
		}
	}
}
