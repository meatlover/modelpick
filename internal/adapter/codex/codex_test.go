package codex

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
)

const (
	fakeAccountID = "acct-FAKE-ID-never-real-77c1"
	fakeUA        = "modelpick/test"
)

var testNow = time.Date(2026, 10, 22, 22, 0, 0, 0, time.UTC)

// makeJWT builds an unsigned JWT whose payload is the given JSON, tagged so
// the whole token is a recognisable fake secret.
func makeJWT(payload string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(payload)) + ".FAKE-SIGNATURE-never-real"
}

func jwtWithExp(exp time.Time) string { return makeJWT(fmt.Sprintf(`{"exp":%d}`, exp.Unix())) }

var fakeToken = jwtWithExp(testNow.Add(time.Hour))

func writeAuth(t *testing.T, home, token string) {
	t.Helper()
	body := fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"access_token":%q,"refresh_token":"r","account_id":%q}}`, token, fakeAccountID)
	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

type fixture struct {
	a       *Adapter
	acct    adapter.Account
	hits    *atomic.Int32
	lastReq *http.Request
}

func newFixture(t *testing.T, handler http.HandlerFunc) *fixture {
	t.Helper()
	f := &fixture{hits: &atomic.Int32{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		f.lastReq = r.Clone(context.Background())
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	home := t.TempDir()
	writeAuth(t, home, fakeToken)
	f.acct = adapter.Account{Name: "cx", Vendor: "codex", CodexHome: home}
	f.a = New(Options{
		BaseURL:   srv.URL,
		HTTP:      srv.Client(),
		Now:       func() time.Time { return testNow },
		LookPath:  func(string) (string, error) { return "/usr/local/bin/codex", nil },
		UserAgent: fakeUA,
	})
	return f
}

func serveBody(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func serveFile(t *testing.T, path string) http.HandlerFunc {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return serveBody(200, string(b))
}

func TestUsageParsesRealResponse(t *testing.T) {
	f := newFixture(t, serveFile(t, "testdata/usage_2026-10-22.json"))
	r, err := f.a.Usage(context.Background(), f.acct)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Windows) != 1 {
		t.Fatalf("windows = %d, want 1: %+v", len(r.Windows), r.Windows)
	}
	w := r.Windows[0]
	if w.Label != "primary_window" || w.Length != 7*24*time.Hour || w.UsedPct != 2 || !w.ResetsAt.Equal(time.Unix(1793294006, 0)) {
		t.Fatalf("window = %+v", w)
	}
	if len(r.Notes) != 0 {
		t.Fatalf("notes = %v, want none", r.Notes)
	}
	if !r.TakenAt.Equal(testNow) {
		t.Fatalf("TakenAt = %v", r.TakenAt)
	}
	req := f.lastReq
	if req.Method != "GET" || req.URL.Path != "/backend-api/wham/usage" {
		t.Fatalf("request = %s %s", req.Method, req.URL.Path)
	}
	for k, want := range map[string]string{
		"Authorization":      "Bearer " + fakeToken,
		"ChatGPT-Account-Id": fakeAccountID,
		"User-Agent":         fakeUA,
		"Accept":             "application/json",
	} {
		if got := req.Header.Get(k); got != want {
			t.Errorf("header %s = %q, want %q", k, got, want)
		}
	}
	if f.hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", f.hits.Load())
	}
}

func TestUsageLimitReachedMeansFull(t *testing.T) {
	win := func(pct int) string {
		return fmt.Sprintf(`{"used_percent":%d,"limit_window_seconds":18000,"reset_at":1793294006}`, pct)
	}
	for name, flags := range map[string]string{
		"limit_reached": `"allowed":true,"limit_reached":true`,
		"not allowed":   `"allowed":false,"limit_reached":false`,
	} {
		t.Run(name, func(t *testing.T) {
			body := `{"rate_limit":{` + flags + `,"primary_window":` + win(40) + `,"secondary_window":` + win(10) + `}}`
			f := newFixture(t, serveBody(200, body))
			r, err := f.a.Usage(context.Background(), f.acct)
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Windows) != 2 {
				t.Fatalf("windows = %+v", r.Windows)
			}
			for _, w := range r.Windows {
				if w.UsedPct != 100 {
					t.Errorf("%s UsedPct = %v, want 100", w.Label, w.UsedPct)
				}
			}
		})
	}
}

func TestUsageNullWindowsSkipped(t *testing.T) {
	f := newFixture(t, serveBody(200, `{"rate_limit":{"allowed":true,"limit_reached":false,"primary_window":null,"secondary_window":null}}`))
	r, err := f.a.Usage(context.Background(), f.acct)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Windows) != 0 {
		t.Fatalf("windows = %+v, want none", r.Windows)
	}
}

func TestUsageAdditionalRateLimitsNoted(t *testing.T) {
	f := newFixture(t, serveBody(200, `{"rate_limit":{"primary_window":{"used_percent":1}},"additional_rate_limits":[{"x":1}]}`))
	r, err := f.a.Usage(context.Background(), f.acct)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Notes, []string{"additional_rate_limits present; not parsed in v1"}) {
		t.Fatalf("notes = %v", r.Notes)
	}
	if r.Windows[0].Length != 0 || !r.Windows[0].ResetsAt.IsZero() {
		t.Fatalf("missing length/reset must be unknown: %+v", r.Windows[0])
	}
}

func TestUsageShapeChange(t *testing.T) {
	for name, body := range map[string]string{
		"no rate_limit":   `{"plan_type":"x"}`,
		"not an object":   `[1,2]`,
		"not json":        `garbage`,
		"no used_percent": `{"rate_limit":{"primary_window":{"limit_window_seconds":1}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, serveBody(200, body))
			_, err := f.a.Usage(context.Background(), f.acct)
			if !errors.Is(err, adapter.ErrShape) {
				t.Fatalf("err = %v, want ErrShape", err)
			}
		})
	}
}

func TestUsage401(t *testing.T) {
	f := newFixture(t, serveBody(401, `{}`))
	_, err := f.a.Usage(context.Background(), f.acct)
	if !errors.Is(err, adapter.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestUsageHTTPErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		retry  string
		want   time.Duration
	}{
		{429, "120", 120 * time.Second},
		{429, "Wed, 21 Oct 2026 07:28:00 GMT", 0},
		{429, "", 0},
		{500, "30", 0},
	} {
		f := newFixture(t, func(w http.ResponseWriter, _ *http.Request) {
			if tc.retry != "" {
				w.Header().Set("Retry-After", tc.retry)
			}
			w.WriteHeader(tc.status)
		})
		_, err := f.a.Usage(context.Background(), f.acct)
		var he *adapter.HTTPError
		if !errors.As(err, &he) || he.Status != tc.status || he.RetryAfter != tc.want {
			t.Errorf("%d/%q: err = %v, want HTTPError{%d, %v}", tc.status, tc.retry, err, tc.status, tc.want)
		}
	}
}

func TestJWTExpiry(t *testing.T) {
	cases := []struct {
		name    string
		token   string
		expired bool
	}{
		{"past exp", jwtWithExp(testNow.Add(-time.Minute)), true},
		{"exp equals now", jwtWithExp(testNow), true},
		{"future exp", jwtWithExp(testNow.Add(time.Minute)), false},
		{"no exp claim", makeJWT(`{"sub":"x"}`), false},
		{"payload not json", makeJWT(`nope`), false},
		{"not a jwt", "opaque-token", false},
		{"bad base64", "a.!!!.c", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, serveBody(200, `{"rate_limit":{}}`))
			writeAuth(t, f.acct.CodexHome, tc.token)
			st := f.a.Credential(f.acct)
			if !st.Present || st.Err != nil || st.Expired != tc.expired {
				t.Fatalf("status = %+v, want present, expired=%v", st, tc.expired)
			}
			_, err := f.a.Usage(context.Background(), f.acct)
			if tc.expired {
				if !errors.Is(err, adapter.ErrTokenExpired) || f.hits.Load() != 0 {
					t.Fatalf("err = %v hits = %d, want ErrTokenExpired and no call", err, f.hits.Load())
				}
			} else if f.hits.Load() != 1 {
				t.Fatalf("hits = %d, want 1", f.hits.Load())
			}
		})
	}
}

func TestCredentialMissingAndMalformed(t *testing.T) {
	a := New(Options{})
	home := t.TempDir()
	acct := adapter.Account{CodexHome: home}
	if st := a.Credential(acct); st.Present || st.Err != nil {
		t.Fatalf("missing file: %+v", st)
	}
	for name, body := range map[string]string{
		"not json":    "{",
		"no tokens":   `{}`,
		"no token":    `{"tokens":{"account_id":"a"}}`,
		"no account":  `{"tokens":{"access_token":"t"}}`,
		"wrong types": `{"tokens":{"access_token":5}}`,
	} {
		if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if st := a.Credential(acct); st.Present || st.Err == nil {
			t.Errorf("%s: %+v, want an Err", name, st)
		}
	}
	if st := a.Credential(adapter.Account{}); st.Present {
		t.Fatalf("empty codex_home: %+v", st)
	}
}

func TestModelsFromCache(t *testing.T) {
	a := New(Options{})
	home := t.TempDir()
	b, err := os.ReadFile("testdata/models_cache.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "models_cache.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	ms, err := a.Models(context.Background(), adapter.Account{CodexHome: home})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	var sol adapter.Model
	for _, m := range ms {
		ids = append(ids, m.ID)
		if m.ID == "gpt-6.1-sol" {
			sol = m
		}
	}
	if !reflect.DeepEqual(ids, []string{"gpt-6.1-sol", "gpt-6-astra", "gpt-6-sol"}) {
		t.Fatalf("ids = %v", ids)
	}
	if want := []string{"low", "medium", "high", "xhigh", "max", "ultra"}; !reflect.DeepEqual(sol.Thinking, want) {
		t.Fatalf("gpt-6.1-sol thinking = %v, want %v", sol.Thinking, want)
	}
}

func TestModelsSkipsHiddenAndReportsErrors(t *testing.T) {
	a := New(Options{})
	home := t.TempDir()
	acct := adapter.Account{CodexHome: home}
	if _, err := a.Models(context.Background(), acct); err == nil {
		t.Fatal("missing cache: want an error")
	}
	cache := `{"models":[
	  {"slug":"shown","visibility":"list","supported_reasoning_levels":[{"effort":"low"}]},
	  {"slug":"hidden","visibility":"hide","supported_reasoning_levels":[{"effort":"low"}]},
	  {"slug":"plain"}]}`
	if err := os.WriteFile(filepath.Join(home, "models_cache.json"), []byte(cache), 0o600); err != nil {
		t.Fatal(err)
	}
	ms, err := a.Models(context.Background(), acct)
	if err != nil {
		t.Fatal(err)
	}
	want := []adapter.Model{{ID: "shown", Thinking: []string{"low"}}, {ID: "plain"}}
	if !reflect.DeepEqual(ms, want) {
		t.Fatalf("models = %+v, want %+v", ms, want)
	}
	if err := os.WriteFile(filepath.Join(home, "models_cache.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Models(context.Background(), acct); !errors.Is(err, adapter.ErrShape) {
		t.Fatalf("corrupt cache: err = %v, want ErrShape", err)
	}
}

func TestCheck(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		f := newFixture(t, serveBody(200, "{}"))
		if got := f.a.Check(context.Background(), f.acct); len(got) != 0 {
			t.Fatalf("findings = %+v", got)
		}
	})
	t.Run("no binary, expired", func(t *testing.T) {
		f := newFixture(t, serveBody(200, "{}"))
		f.a.o.LookPath = func(string) (string, error) { return "", errors.New("nope") }
		writeAuth(t, f.acct.CodexHome, jwtWithExp(testNow.Add(-time.Hour)))
		got := f.a.Check(context.Background(), f.acct)
		if len(got) != 2 || got[0].Severity != adapter.SeverityError || got[1].Severity != adapter.SeverityWarning || got[1].Account != "cx" {
			t.Fatalf("findings = %+v", got)
		}
	})
	t.Run("missing auth", func(t *testing.T) {
		f := newFixture(t, serveBody(200, "{}"))
		if err := os.Remove(filepath.Join(f.acct.CodexHome, "auth.json")); err != nil {
			t.Fatal(err)
		}
		got := f.a.Check(context.Background(), f.acct)
		if len(got) != 1 || got[0].Severity != adapter.SeverityError {
			t.Fatalf("findings = %+v", got)
		}
	})
}

func TestVendor(t *testing.T) {
	if v := New(Options{}).Vendor(); v != "codex" {
		t.Fatal(v)
	}
}

func TestErrorsNeverContainToken(t *testing.T) {
	secrets := []string{fakeToken, fakeAccountID}
	echo := func(status int) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(status)
			_, _ = fmt.Fprintf(w, `{"echo":%q,"acct":%q}`, r.Header.Get("Authorization"), r.Header.Get("ChatGPT-Account-Id"))
		}
	}
	probes := []struct {
		name    string
		handler http.HandlerFunc
		auth    string // raw auth.json override
		dead    bool
	}{
		{name: "401", handler: echo(401)},
		{name: "429", handler: echo(429)},
		{name: "500", handler: echo(500)},
		{name: "shape", handler: echo(200)},
		{name: "not json", handler: serveBody(200, "garbage "+fakeToken+fakeAccountID)},
		{name: "network", handler: serveBody(200, "{}"), dead: true},
		{name: "expired", handler: serveBody(200, "{}"), auth: fmt.Sprintf(`{"tokens":{"access_token":%q,"account_id":%q}}`, jwtWithExp(testNow.Add(-time.Hour)), fakeAccountID)},
		{name: "truncated auth", handler: serveBody(200, "{}"), auth: `{"tokens":{"access_token":"` + fakeToken + `","account_id":"` + fakeAccountID},
		{name: "wrong type", handler: serveBody(200, "{}"), auth: `{"tokens":{"access_token":"` + fakeToken + `","account_id":["` + fakeAccountID + `"]}}`},
	}
	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			f := newFixture(t, p.handler)
			if p.auth != "" {
				if err := os.WriteFile(filepath.Join(f.acct.CodexHome, "auth.json"), []byte(p.auth), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if p.dead {
				f.a.o.BaseURL = "http://127.0.0.1:1"
			}
			_, err := f.a.Usage(context.Background(), f.acct)
			if err == nil {
				t.Fatal("want an error")
			}
			texts := []string{err.Error()}
			st := f.a.Credential(f.acct)
			texts = append(texts, st.Source)
			if st.Err != nil {
				texts = append(texts, st.Err.Error())
			}
			for _, fd := range f.a.Check(context.Background(), f.acct) {
				texts = append(texts, fd.Message)
			}
			for _, txt := range texts {
				for _, s := range secrets {
					if strings.Contains(txt, s) {
						t.Fatalf("text leaks a secret: %q", txt)
					}
				}
			}
		})
	}
}
