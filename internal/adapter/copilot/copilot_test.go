package copilot

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
)

const testToken = "gho_TESTTOKEN"

type fixture struct {
	a    *Adapter
	hits *atomic.Int32
	last *http.Request
}

func newFixture(t *testing.T, handler http.HandlerFunc, auth string) *fixture {
	t.Helper()
	f := &fixture{hits: new(atomic.Int32)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		f.last = r
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	path := filepath.Join(t.TempDir(), "auth.json")
	if auth != "" {
		if err := os.WriteFile(path, []byte(auth), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f.a = New(Options{
		BaseURL: srv.URL, AuthFile: path, UserAgent: "test-ua",
		Now: func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) },
	})
	return f
}

const goodAuth = `{"github-copilot":{"type":"oauth","refresh":"` + testToken + `","access":"a","expires":0}}`

func serveBody(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func fixtureBody(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/usage.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUsageFixture(t *testing.T) {
	f := newFixture(t, serveBody(200, fixtureBody(t)), goodAuth)
	r, err := f.a.Usage(context.Background(), adapter.Account{Vendor: "copilot"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Windows) != 1 {
		t.Fatalf("windows = %+v", r.Windows)
	}
	w := r.Windows[0]
	reset := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	if w.Label != "premium_interactions" || w.UsedPct < 0.39 || w.UsedPct > 0.41 ||
		!w.ResetsAt.Equal(reset) || w.Length != 30*24*time.Hour || w.ScopeModelID != "" || w.ScopeModelName != "" {
		t.Fatalf("window = %+v", w)
	}
	if !r.TakenAt.Equal(time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("TakenAt = %v", r.TakenAt)
	}
	if f.hits.Load() != 1 || f.last.URL.Path != "/copilot_internal/user" || f.last.Method != "GET" {
		t.Fatalf("hits=%d path=%s", f.hits.Load(), f.last.URL.Path)
	}
	if got := f.last.Header.Get("Authorization"); got != "token "+testToken {
		t.Fatalf("Authorization = %q", got)
	}
	if f.last.Header.Get("User-Agent") != "test-ua" || f.last.Header.Get("Accept") != "application/json" {
		t.Fatalf("headers = %v", f.last.Header)
	}
}

func TestDefaultUserAgent(t *testing.T) {
	if got := New(Options{}).o.UserAgent; got != "GitHubCopilotChat/0.26.7" {
		t.Fatalf("UserAgent = %q", got)
	}
}

func snap(extra string) string {
	return `{"quota_reset_date":"2026-12-01","quota_snapshots":{"premium_interactions":{` + extra + `}}}`
}

func TestParseUsage(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		used  float64
		note  string
		reset bool
	}{
		{"negative remaining clamps to 100 used", snap(`"percent_remaining":-5`), 100, "", true},
		{"over 100 remaining clamps to 0 used", snap(`"percent_remaining":180`), 0, "", true},
		{"unlimited", snap(`"unlimited":true,"percent_remaining":50`), 0, "premium_interactions unlimited", true},
		{"bad reset date", `{"quota_reset_date":"soon","quota_snapshots":{"premium_interactions":{"percent_remaining":50}}}`, 50, "quota_reset_date", false},
		{"missing reset date", `{"quota_snapshots":{"premium_interactions":{"percent_remaining":50}}}`, 50, "quota_reset_date", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := parseUsage([]byte(c.body))
			if err != nil {
				t.Fatal(err)
			}
			w := r.Windows[0]
			if w.UsedPct != c.used {
				t.Fatalf("UsedPct = %v, want %v", w.UsedPct, c.used)
			}
			if c.reset != !w.ResetsAt.IsZero() || c.reset != (w.Length > 0) {
				t.Fatalf("window = %+v", w)
			}
			joined := strings.Join(r.Notes, "|")
			if (c.note == "") != (joined == "") || !strings.Contains(joined, c.note) {
				t.Fatalf("notes = %q, want %q", joined, c.note)
			}
		})
	}
}

func TestParseUsageLengthIsCalendarMonth(t *testing.T) {
	r, err := parseUsage([]byte(`{"quota_reset_date":"2026-03-01","quota_snapshots":{"premium_interactions":{"percent_remaining":10}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Windows[0].Length; got != 28*24*time.Hour {
		t.Fatalf("Length = %v, want 28 days (February)", got)
	}
}

func TestParseUsageShapeErrors(t *testing.T) {
	bodies := map[string]string{
		"no premium_interactions": `{"quota_snapshots":{"chat":{"unlimited":true}}}`,
		"no snapshots":            `{"copilot_plan":"business"}`,
		"not json":                `<html>nope</html>`,
		"no percent":              snap(`"entitlement":5`),
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			_, err := parseUsage([]byte(body))
			if !errors.Is(err, adapter.ErrShape) {
				t.Fatalf("err = %v, want ErrShape", err)
			}
			if name == "no premium_interactions" && !strings.Contains(err.Error(), "premium_interactions") {
				t.Fatalf("err does not name the key: %v", err)
			}
			if strings.Contains(err.Error(), "nope") {
				t.Fatalf("err quotes the body: %v", err)
			}
		})
	}
}

func TestUsageStatuses(t *testing.T) {
	t.Run("401", func(t *testing.T) {
		f := newFixture(t, serveBody(401, "{}"), goodAuth)
		if _, err := f.a.Usage(context.Background(), adapter.Account{}); !errors.Is(err, adapter.ErrUnauthorized) {
			t.Fatalf("err = %v", err)
		}
	})
	for _, code := range []int{403, 404} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			f := newFixture(t, serveBody(code, "{}"), goodAuth)
			_, err := f.a.Usage(context.Background(), adapter.Account{})
			var he *adapter.HTTPError
			if err == nil || !strings.Contains(err.Error(), "endpoint may have changed") || !errors.As(err, &he) || he.Status != code {
				t.Fatalf("err = %v", err)
			}
		})
	}
	t.Run("429", func(t *testing.T) {
		f := newFixture(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(429)
		}, goodAuth)
		_, err := f.a.Usage(context.Background(), adapter.Account{})
		var he *adapter.HTTPError
		if !errors.As(err, &he) || he.Status != 429 || he.RetryAfter != 60*time.Second {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("500", func(t *testing.T) {
		f := newFixture(t, serveBody(500, "{}"), goodAuth)
		_, err := f.a.Usage(context.Background(), adapter.Account{})
		var he *adapter.HTTPError
		if !errors.As(err, &he) || he.Status != 500 || strings.Contains(err.Error(), "endpoint may have changed") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("malformed body", func(t *testing.T) {
		f := newFixture(t, serveBody(200, "garbage"), goodAuth)
		if _, err := f.a.Usage(context.Background(), adapter.Account{}); !errors.Is(err, adapter.ErrShape) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestUsageNoCredentialMakesNoCall(t *testing.T) {
	for name, auth := range map[string]string{"missing file": "", "no entry": `{}`, "bad type": `{"github-copilot":{"type":"api"}}`} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, serveBody(200, fixtureBody(t)), auth)
			if _, err := f.a.Usage(context.Background(), adapter.Account{}); err == nil {
				t.Fatal("want an error")
			}
			if f.hits.Load() != 0 {
				t.Fatalf("server hit %d times", f.hits.Load())
			}
		})
	}
}

func TestCheck(t *testing.T) {
	f := newFixture(t, serveBody(200, "{}"), goodAuth)
	f.a.o.LookPath = func(string) (string, error) { return "/bin/opencode", nil }
	if got := f.a.Check(context.Background(), adapter.Account{Name: "c"}); len(got) != 0 {
		t.Fatalf("healthy findings = %+v", got)
	}
	f.a.o.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	got := f.a.Check(context.Background(), adapter.Account{Name: "c"})
	if len(got) != 1 || got[0].Severity != adapter.SeverityWarning || got[0].Account != "c" ||
		!strings.Contains(got[0].Message, "opencode not found on PATH") {
		t.Fatalf("no-binary findings = %+v", got)
	}
	g := newFixture(t, serveBody(200, "{}"), "")
	g.a.o.LookPath = func(string) (string, error) { return "/bin/opencode", nil }
	got = g.a.Check(context.Background(), adapter.Account{Name: "c"})
	if len(got) != 1 || got[0].Severity != adapter.SeverityError || !strings.Contains(got[0].Message, "opencode auth login") {
		t.Fatalf("no-credential findings = %+v", got)
	}
	m := newFixture(t, serveBody(200, "{}"), `{"github-copilot": gho_SECRETSECRET`)
	m.a.o.LookPath = func(string) (string, error) { return "/bin/opencode", nil }
	got = m.a.Check(context.Background(), adapter.Account{Name: "c"})
	if len(got) != 1 || got[0].Severity != adapter.SeverityError || !strings.Contains(got[0].Message, "credential unreadable") ||
		strings.Contains(got[0].Message, "SECRET") {
		t.Fatalf("malformed-auth findings = %+v", got)
	}
}

func TestUsageCredentialErrorLeaksNoToken(t *testing.T) {
	f := newFixture(t, serveBody(200, fixtureBody(t)), `{"github-copilot": gho_SECRETSECRET`)
	_, err := f.a.Usage(context.Background(), adapter.Account{})
	if err == nil || !strings.Contains(err.Error(), "credential") || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err = %v", err)
	}
	if f.hits.Load() != 0 {
		t.Fatalf("server hit %d times", f.hits.Load())
	}
}
