package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
)

const fakeToken = "sk-ant-oat01-FAKE-TOKEN-never-real-3f9a"

var testNow = time.Date(2026, 10, 22, 22, 0, 0, 0, time.UTC)

func credJSON(token string, expires time.Time) string {
	return fmt.Sprintf(`{"claudeAiOauth":{"accessToken":%q,"refreshToken":"r","expiresAt":%d,"scopes":["user:inference"]}}`,
		token, expires.UnixMilli())
}

// keychainWith returns a fake Keychain that answers secret for any service and
// records the service it was asked for.
func keychainWith(secret string, found bool, err error, asked *string) func(context.Context, string) (string, bool, error) {
	return func(_ context.Context, service string) (string, bool, error) {
		if asked != nil {
			*asked = service
		}
		return secret, found, err
	}
}

type fixture struct {
	a        *Adapter
	acct     adapter.Account
	hits     *atomic.Int32
	versions *atomic.Int32
	lastReq  *http.Request
}

// newFixture builds an adapter against an httptest server that runs handler.
// The credential comes from a fake Keychain holding a valid fake token.
func newFixture(t *testing.T, handler http.HandlerFunc) *fixture {
	t.Helper()
	f := &fixture{hits: &atomic.Int32{}, versions: &atomic.Int32{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		f.lastReq = r.Clone(context.Background())
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	home := t.TempDir()
	f.acct = adapter.Account{Name: "work", Vendor: "claude", ConfigDir: filepath.Join(home, ".claude-work")}
	f.a = New(Options{
		BaseURL:  srv.URL,
		HTTP:     srv.Client(),
		Home:     home,
		Now:      func() time.Time { return testNow },
		Keychain: keychainWith(credJSON(fakeToken, testNow.Add(time.Hour)), true, nil, nil),
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			f.versions.Add(1)
			if name != "claude" || strings.Join(args, " ") != "--version" {
				return nil, fmt.Errorf("unexpected command %s %v", name, args)
			}
			return []byte("2.1.37 (Claude Code)\n"), nil
		},
		LookPath: func(string) (string, error) { return "/usr/local/bin/claude", nil },
	})
	return f
}

func serveFile(t *testing.T, path string) http.HandlerFunc {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return serveBody(http.StatusOK, string(body))
}

func serveBody(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func findWindow(t *testing.T, ws []adapter.Window, length time.Duration, scopeName string) adapter.Window {
	t.Helper()
	var hits []adapter.Window
	for _, w := range ws {
		if w.Length == length && w.ScopeModelName == scopeName {
			hits = append(hits, w)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("want exactly 1 window length=%v scope=%q, got %d in %+v", length, scopeName, len(hits), ws)
	}
	return hits[0]
}

func TestKeychainServiceKnownPairs(t *testing.T) {
	const home = "/Users/meatlover"
	cases := map[string]string{
		"/Users/meatlover/.claude-rec": "Claude Code-credentials-6a4e8c1e",
		"/Users/meatlover/.claude":     "Claude Code-credentials",
	}
	for dir, want := range cases {
		if got := KeychainService(dir, home); got != want {
			t.Errorf("KeychainService(%q) = %q, want %q", dir, got, want)
		}
	}
}

func TestKeychainServiceNormalisesPath(t *testing.T) {
	const home = "/Users/meatlover"
	cases := map[string]string{
		"/Users/meatlover/.claude-rec/":     "Claude Code-credentials-6a4e8c1e",
		"/Users/meatlover/x/../.claude-rec": "Claude Code-credentials-6a4e8c1e",
		"/Users/meatlover/.claude/":         "Claude Code-credentials",
		"/Users/meatlover/./.claude":        "Claude Code-credentials",
	}
	for dir, want := range cases {
		if got := KeychainService(dir, home); got != want {
			t.Errorf("KeychainService(%q) = %q, want %q", dir, got, want)
		}
	}
	// A home with a trailing slash still recognises the default dir.
	if got := KeychainService("/Users/meatlover/.claude", home+"/"); got != "Claude Code-credentials" {
		t.Errorf("home with trailing slash: got %q", got)
	}
}

func TestUsageParsesRealResponse(t *testing.T) {
	f := newFixture(t, serveFile(t, "testdata/usage_2026-10-22.json"))
	r, err := f.a.Usage(context.Background(), f.acct)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	req := f.lastReq
	if req.Method != http.MethodGet || req.URL.Path != "/api/oauth/usage" {
		t.Errorf("request = %s %s, want GET /api/oauth/usage", req.Method, req.URL.Path)
	}
	checks := map[string]string{
		"Authorization":  "Bearer " + fakeToken,
		"Anthropic-Beta": "oauth-2025-04-20",
		"Accept":         "application/json",
		"User-Agent":     "claude-cli/2.1.37 (external, cli)",
	}
	for h, want := range checks {
		if got := req.Header.Get(h); got != want {
			t.Errorf("header %s = %q, want %q", h, got, want)
		}
	}
	if !strings.HasPrefix(req.Header.Get("User-Agent"), "claude-cli/") {
		t.Errorf("User-Agent lacks claude-cli/ prefix")
	}
	if !r.TakenAt.Equal(testNow) {
		t.Errorf("TakenAt = %v, want %v", r.TakenAt, testNow)
	}
	if len(r.Windows) != 3 {
		t.Fatalf("want 3 windows, got %d: %+v", len(r.Windows), r.Windows)
	}
	if len(r.Notes) != 0 {
		t.Errorf("unexpected notes: %q", r.Notes)
	}

	s := findWindow(t, r.Windows, 5*time.Hour, "")
	if s.UsedPct != 11 || !s.ResetsAt.Equal(mustTime(t, "2026-10-23T01:19:59.653578Z")) || s.Label != "session" {
		t.Errorf("session window = %+v", s)
	}
	wk := findWindow(t, r.Windows, 7*24*time.Hour, "")
	if wk.UsedPct != 2 || !wk.ResetsAt.Equal(mustTime(t, "2026-10-28T09:59:59.653599Z")) || wk.ScopeModelID != "" {
		t.Errorf("weekly window = %+v", wk)
	}
	fb := findWindow(t, r.Windows, 7*24*time.Hour, "Fable")
	if fb.UsedPct != 0 || fb.ScopeModelID != "" || !fb.ResetsAt.Equal(mustTime(t, "2026-10-28T10:00:00Z")) {
		t.Errorf("Fable window = %+v", fb)
	}

	// The CLI version is computed once per adapter.
	if _, err := f.a.Usage(context.Background(), f.acct); err != nil {
		t.Fatal(err)
	}
	if got := f.versions.Load(); got != 1 {
		t.Errorf("claude --version ran %d times over two calls, want 1", got)
	}
}

func TestUserAgentFallsBackWhenVersionUnavailable(t *testing.T) {
	f := newFixture(t, serveFile(t, "testdata/usage_2026-10-22.json"))
	f.a.o.Run = func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("exec: \"claude\": executable file not found in $PATH")
	}
	if _, err := f.a.Usage(context.Background(), f.acct); err != nil {
		t.Fatal(err)
	}
	if got := f.lastReq.Header.Get("User-Agent"); got != "claude-cli/2.1.0 (external, cli)" {
		t.Errorf("User-Agent = %q, want fallback 2.1.0", got)
	}
}

func fixtureWithout(t *testing.T, keys ...string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/usage_2026-10-22.json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Fatalf("fixture has no key %q", k)
		}
		delete(m, k)
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestUsageFallsBackToNamedFields(t *testing.T) {
	f := newFixture(t, serveBody(http.StatusOK, fixtureWithout(t, "limits")))
	r, err := f.a.Usage(context.Background(), f.acct)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if len(r.Windows) != 2 {
		t.Fatalf("want 2 windows, got %+v", r.Windows)
	}
	s := findWindow(t, r.Windows, 5*time.Hour, "")
	if s.UsedPct != 11 || !s.ResetsAt.Equal(mustTime(t, "2026-10-23T01:19:59.653578Z")) {
		t.Errorf("five_hour window = %+v", s)
	}
	w := findWindow(t, r.Windows, 7*24*time.Hour, "")
	if w.UsedPct != 2 || !w.ResetsAt.Equal(mustTime(t, "2026-10-28T09:59:59.653599Z")) {
		t.Errorf("seven_day window = %+v", w)
	}
}

func TestUsageShapeChange(t *testing.T) {
	bodies := map[string]string{
		"unknown object":          `{"foo":1}`,
		"not json":                `<html>maintenance</html>`,
		"limits and named absent": fixtureWithout(t, "limits", "five_hour", "seven_day"),
		"limit without percent":   `{"limits":[{"kind":"session","group":"session","resets_at":null,"scope":null}]}`,
		"named without value":     `{"five_hour":{"utilization":null,"resets_at":null},"seven_day":null}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, serveBody(http.StatusOK, body))
			_, err := f.a.Usage(context.Background(), f.acct)
			if !errors.Is(err, adapter.ErrShape) {
				t.Fatalf("want ErrShape, got %v", err)
			}
		})
	}
}

func TestUsage401(t *testing.T) {
	f := newFixture(t, serveBody(http.StatusUnauthorized,
		`{"type":"error","error":{"type":"authentication_error","message":"OAuth token has expired."}}`))
	_, err := f.a.Usage(context.Background(), f.acct)
	if !errors.Is(err, adapter.ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
}

func TestUsage429RetryAfterIsClamped(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "10000000000")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := f.a.Usage(context.Background(), f.acct)
	var he *adapter.HTTPError
	if !errors.As(err, &he) || he.Status != 429 || he.RetryAfter != 24*time.Hour {
		t.Fatalf("want HTTPError{429, 24h}, got %v (%+v)", err, he)
	}
}

func TestUsage429RetryAfter(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := f.a.Usage(context.Background(), f.acct)
	var he *adapter.HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("want *HTTPError, got %v", err)
	}
	if he.Status != 429 || he.RetryAfter != 2*time.Minute {
		t.Fatalf("got %+v, want {429 2m}", he)
	}
}

func TestUsageOtherStatusIsHTTPError(t *testing.T) {
	f := newFixture(t, serveBody(http.StatusInternalServerError, `{"error":"boom"}`))
	_, err := f.a.Usage(context.Background(), f.acct)
	var he *adapter.HTTPError
	if !errors.As(err, &he) || he.Status != 500 || he.RetryAfter != 0 {
		t.Fatalf("want *HTTPError{500, 0}, got %v", err)
	}
}

func TestUnknownGroupNoted(t *testing.T) {
	body := `{"limits":[
		{"kind":"session","group":"session","percent":40,"resets_at":"2026-10-23T01:00:00Z","scope":null},
		{"kind":"monthly_all","group":"monthly","percent":7,"resets_at":"2026-11-01T00:00:00Z","scope":null}
	]}`
	f := newFixture(t, serveBody(http.StatusOK, body))
	r, err := f.a.Usage(context.Background(), f.acct)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Windows) != 2 {
		t.Fatalf("want 2 windows, got %+v", r.Windows)
	}
	u := findWindow(t, r.Windows, 0, "")
	if u.UsedPct != 7 || u.Label != "monthly_all" {
		t.Errorf("unknown-group window = %+v", u)
	}
	want := `unrecognised window group "monthly"`
	if len(r.Notes) != 1 || r.Notes[0] != want {
		t.Errorf("Notes = %q, want [%q]", r.Notes, want)
	}
}

func TestForeignSurfaceDropped(t *testing.T) {
	body := `{"limits":[
		{"kind":"session","group":"session","percent":40,"resets_at":null,"scope":null},
		{"kind":"weekly_chat","group":"weekly","percent":99,"resets_at":null,"scope":{"model":null,"surface":"chat"}},
		{"kind":"weekly_cc","group":"weekly","percent":30,"resets_at":null,"scope":{"model":{"id":"claude-opus-5-5","display_name":"Opus"},"surface":"claude_code"}}
	]}`
	f := newFixture(t, serveBody(http.StatusOK, body))
	r, err := f.a.Usage(context.Background(), f.acct)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Windows) != 2 {
		t.Fatalf("want 2 windows (chat surface dropped), got %+v", r.Windows)
	}
	cc := findWindow(t, r.Windows, 7*24*time.Hour, "Opus")
	if cc.UsedPct != 30 || cc.ScopeModelID != "claude-opus-5-5" {
		t.Errorf("claude_code window = %+v", cc)
	}
	s := findWindow(t, r.Windows, 5*time.Hour, "")
	if !s.ResetsAt.IsZero() {
		t.Errorf("null resets_at should be zero, got %v", s.ResetsAt)
	}
}

func TestCredentialFileFallbackAndExpiry(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude-rec")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	credPath := filepath.Join(dir, ".credentials.json")
	if err := os.WriteFile(credPath, []byte(credJSON(fakeToken, testNow.Add(-time.Minute))), 0o600); err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1) }))
	defer srv.Close()

	var asked string
	a := New(Options{
		BaseURL:  srv.URL,
		HTTP:     srv.Client(),
		Home:     home,
		Now:      func() time.Time { return testNow },
		Keychain: keychainWith("", false, nil, &asked),
		Run:      func(context.Context, string, ...string) ([]byte, error) { return []byte("2.1.37\n"), nil },
		LookPath: func(string) (string, error) { return "/bin/claude", nil },
	})
	acct := adapter.Account{Name: "rec", Vendor: "claude", ConfigDir: dir + "/"}

	st := a.Credential(acct)
	if !st.Present || !st.Expired || st.Err != nil {
		t.Fatalf("want Present && Expired, got %+v", st)
	}
	if want := KeychainService(dir, home); asked != want {
		t.Errorf("Keychain asked for %q, want %q", asked, want)
	}
	if st.Source != "file: "+credPath {
		t.Errorf("Source = %q, want file: %s", st.Source, credPath)
	}

	if _, err := a.Usage(context.Background(), acct); !errors.Is(err, adapter.ErrTokenExpired) {
		t.Fatalf("Usage with expired token: want ErrTokenExpired, got %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("expired token must not reach the endpoint; hits = %d", hits.Load())
	}

	// A Keychain item, when present, wins over the file.
	a.o.Keychain = keychainWith(credJSON(fakeToken, testNow.Add(time.Hour)), true, nil, nil)
	st = a.Credential(acct)
	if !st.Present || st.Expired || st.Source != "keychain: "+KeychainService(dir, home) {
		t.Fatalf("keychain first: got %+v", st)
	}

	// Neither source: not present, and Usage refuses without calling.
	if err := os.Remove(credPath); err != nil {
		t.Fatal(err)
	}
	a.o.Keychain = keychainWith("", false, nil, nil)
	st = a.Credential(acct)
	if st.Present || st.Err != nil {
		t.Fatalf("no credential: got %+v", st)
	}
	if _, err := a.Usage(context.Background(), acct); err == nil {
		t.Fatal("Usage without credential: want error")
	}
	if hits.Load() != 0 {
		t.Fatalf("missing credential must not reach the endpoint; hits = %d", hits.Load())
	}
}

func TestCredentialUnreadable(t *testing.T) {
	cases := map[string]func(context.Context, string) (string, bool, error){
		"keychain error":      keychainWith("", false, errors.New("keychain lookup failed: exit status 51"), nil),
		"truncated json":      keychainWith(`{"claudeAiOauth":{"accessToken":"`+fakeToken, true, nil, nil),
		"no access token":     keychainWith(`{"claudeAiOauth":{"expiresAt":1}}`, true, nil, nil),
		"wrong type of token": keychainWith(`{"claudeAiOauth":{"accessToken":12}}`, true, nil, nil),
	}
	for name, kc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, serveBody(http.StatusOK, `{}`))
			f.a.o.Keychain = kc
			st := f.a.Credential(f.acct)
			if st.Err == nil || st.Present {
				t.Fatalf("want Err and !Present, got %+v", st)
			}
			if _, err := f.a.Usage(context.Background(), f.acct); err == nil {
				t.Fatal("Usage: want error")
			}
			if f.hits.Load() != 0 {
				t.Fatalf("unreadable credential reached the endpoint")
			}
		})
	}
}

func TestErrorsNeverContainToken(t *testing.T) {
	type probe struct {
		name    string
		handler http.HandlerFunc
		kc      func(context.Context, string) (string, bool, error)
		dead    bool // server closed before the call
	}
	echo := func(status int) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(status)
			_, _ = fmt.Fprintf(w, `{"echo":%q}`, r.Header.Get("Authorization"))
		}
	}
	probes := []probe{
		{name: "401", handler: echo(401)},
		{name: "429", handler: echo(429)},
		{name: "500", handler: echo(500)},
		{name: "403", handler: echo(403)},
		{name: "shape", handler: echo(200)},
		{name: "not json", handler: serveBody(200, "garbage "+fakeToken)},
		{name: "network", handler: serveBody(200, "{}"), dead: true},
		{name: "expired", handler: serveBody(200, "{}"), kc: keychainWith(credJSON(fakeToken, testNow.Add(-time.Hour)), true, nil, nil)},
		{name: "truncated credential", handler: serveBody(200, "{}"), kc: keychainWith(`{"claudeAiOauth":{"accessToken":"`+fakeToken, true, nil, nil)},
		{name: "bad expiresAt", handler: serveBody(200, "{}"), kc: keychainWith(`{"claudeAiOauth":{"accessToken":"`+fakeToken+`","expiresAt":"`+fakeToken+`"}}`, true, nil, nil)},
	}
	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			f := newFixture(t, p.handler)
			if p.kc != nil {
				f.a.o.Keychain = p.kc
			}
			if p.dead {
				f.a.o.BaseURL = "http://127.0.0.1:1" // nothing listens on port 1
			}
			_, err := f.a.Usage(context.Background(), f.acct)
			if err == nil {
				t.Fatal("want an error")
			}
			if strings.Contains(err.Error(), fakeToken) {
				t.Fatalf("error leaks the token: %v", err)
			}
			st := f.a.Credential(f.acct)
			if st.Err != nil && strings.Contains(st.Err.Error(), fakeToken) {
				t.Fatalf("credential error leaks the token: %v", st.Err)
			}
			if strings.Contains(st.Source, fakeToken) {
				t.Fatalf("Source leaks the token: %q", st.Source)
			}
			for _, fd := range f.a.Check(context.Background(), f.acct) {
				if strings.Contains(fd.Message, fakeToken) {
					t.Fatalf("finding leaks the token: %q", fd.Message)
				}
			}
		})
	}
}

func TestCheck(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		f := newFixture(t, serveBody(200, "{}"))
		if got := f.a.Check(context.Background(), f.acct); len(got) != 0 {
			t.Fatalf("want no findings, got %+v", got)
		}
		if f.hits.Load() != 0 {
			t.Fatal("Check must not call the usage endpoint")
		}
	})
	t.Run("binary missing and no credential", func(t *testing.T) {
		f := newFixture(t, serveBody(200, "{}"))
		f.a.o.LookPath = func(string) (string, error) { return "", errors.New("not found") }
		f.a.o.Keychain = keychainWith("", false, nil, nil)
		got := f.a.Check(context.Background(), f.acct)
		if len(got) != 2 {
			t.Fatalf("want 2 findings, got %+v", got)
		}
		for _, fd := range got {
			if fd.Severity != adapter.SeverityError || fd.Account != "work" {
				t.Errorf("finding = %+v, want error for account work", fd)
			}
		}
		if !strings.Contains(got[0].Message, "claude") || !strings.Contains(got[0].Message, "PATH") {
			t.Errorf("PATH finding = %q", got[0].Message)
		}
		if !strings.Contains(got[1].Message, KeychainService(f.acct.ConfigDir, f.a.o.Home)) ||
			!strings.Contains(got[1].Message, filepath.Join(f.acct.ConfigDir, ".credentials.json")) {
			t.Errorf("credential finding should name both sources: %q", got[1].Message)
		}
	})
	t.Run("unreadable credential", func(t *testing.T) {
		f := newFixture(t, serveBody(200, "{}"))
		f.a.o.Keychain = keychainWith("not json", true, nil, nil)
		got := f.a.Check(context.Background(), f.acct)
		if len(got) != 1 || got[0].Severity != adapter.SeverityError {
			t.Fatalf("want 1 error finding, got %+v", got)
		}
	})
	t.Run("expired", func(t *testing.T) {
		f := newFixture(t, serveBody(200, "{}"))
		f.a.o.Keychain = keychainWith(credJSON(fakeToken, testNow.Add(-time.Second)), true, nil, nil)
		got := f.a.Check(context.Background(), f.acct)
		want := adapter.Finding{Severity: adapter.SeverityWarning, Account: "work",
			Message: "token expired — the next usage call asks Claude Code to renew it; if that fails, log in again with /login"}
		if len(got) != 1 || got[0] != want {
			t.Fatalf("got %+v, want [%+v]", got, want)
		}
	})
}

func TestModels(t *testing.T) {
	a := New(Options{
		Home: t.TempDir(),
		Keychain: func(context.Context, string) (string, bool, error) {
			t.Fatal("Models must not read a credential")
			return "", false, nil
		},
		Run: func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("Models must not run a program")
			return nil, nil
		},
	})
	ms, err := a.Models(context.Background(), adapter.Account{})
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "claude-fable-5-1"}
	if len(ms) != len(wantIDs) {
		t.Fatalf("got %d models, want %d", len(ms), len(wantIDs))
	}
	for i, m := range ms {
		if m.ID != wantIDs[i] {
			t.Errorf("model %d = %q, want %q", i, m.ID, wantIDs[i])
		}
		if strings.Join(m.Thinking, ",") != "low,medium,high,xhigh,max" {
			t.Errorf("%s thinking = %v", m.ID, m.Thinking)
		}
	}
	// Callers cannot corrupt the embedded list.
	ms[0].Thinking[0] = "mutated"
	ms[0].ID = "mutated"
	again, _ := a.Models(context.Background(), adapter.Account{})
	if again[0].ID != "claude-opus-5-5" || again[0].Thinking[0] != "low" {
		t.Fatalf("Models returned shared state: %+v", again[0])
	}
	if a.Vendor() != "claude" {
		t.Errorf("Vendor() = %q", a.Vendor())
	}
}

var _ adapter.Adapter = (*Adapter)(nil)

func TestUserAgentProbeNotCachedOnFailure(t *testing.T) {
	calls := 0
	a := New(Options{Run: func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
		calls++
		if err := ctx.Err(); err != nil {
			return nil, err // the probe has its own context, never a request's
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("probe has no timeout of its own")
		}
		if calls == 1 {
			return nil, errors.New("not found")
		}
		return []byte("2.1.99 (Claude Code)\n"), nil
	}})

	if got, want := a.userAgent(), "claude-cli/"+fallbackVersion+" (external, cli)"; got != want {
		t.Fatalf("failed probe: UA = %q, want fallback %q", got, want)
	}
	if got, want := a.userAgent(), "claude-cli/2.1.99 (external, cli)"; got != want {
		t.Fatalf("second probe: UA = %q, want %q", got, want)
	}
	a.userAgent()
	if calls != 2 {
		t.Errorf("probe ran %d times, want 2 (success is cached)", calls)
	}
}

func TestIsDefaultConfigDir(t *testing.T) {
	for dir, want := range map[string]bool{
		"/home/u/.claude":      true,
		"/home/u/.claude/":     true,
		"/home/u/x/../.claude": true,
		"/home/u/.claude-rec":  false,
		"/other/.claude":       false,
	} {
		if got := IsDefaultConfigDir(dir, "/home/u"); got != want {
			t.Errorf("IsDefaultConfigDir(%q) = %v, want %v", dir, got, want)
		}
	}
}

func TestCredentialLoginExpires(t *testing.T) {
	fixture, err := os.ReadFile("testdata/credentials_refresh_expiry.json")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		blob string
		want time.Time
	}{
		{"present", string(fixture), time.UnixMilli(1795000000000)},
		{"absent", credJSON(fakeToken, testNow.Add(time.Hour)), time.Time{}},
		{"zero", `{"claudeAiOauth":{"accessToken":"` + fakeToken + `","refreshTokenExpiresAt":0}}`, time.Time{}},
		{"negative", `{"claudeAiOauth":{"accessToken":"` + fakeToken + `","refreshTokenExpiresAt":-5}}`, time.Time{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, serveBody(200, "{}"))
			f.a.o.Keychain = keychainWith(c.blob, true, nil, nil)
			st := f.a.Credential(f.acct)
			if !st.Present || st.Err != nil {
				t.Fatalf("credential: %+v", st)
			}
			if !st.LoginExpires.Equal(c.want) {
				t.Errorf("LoginExpires = %v, want %v", st.LoginExpires, c.want)
			}
		})
	}
}
