package opencodego

import (
	"context"
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
	fakeKey1 = "FAKE-KEY-ONE-never-real-aa11"
	fakeKey2 = "FAKE-KEY-TWO-never-real-bb22"
	fakeKey3 = "FAKE-KEY-THREE-never-real-cc33"
	fakeUA   = "modelpick/test"
)

var testNow = time.Date(2026, 10, 22, 22, 0, 0, 0, time.UTC)

func writeKeys(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "keys")
	body := "# opencode go keys\n\n" + fakeKey1 + "\n\n  " + fakeKey2 + "  \n# spare\n" + fakeKey3 + "\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

type fixture struct {
	a       *Adapter
	acct    adapter.Account
	hits    *atomic.Int32
	lastReq *http.Request
	runName string
	runArgs []string
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
	f.acct = adapter.Account{Name: "go-2", Vendor: "opencode-go", KeyFile: writeKeys(t), KeyLine: 2}
	f.a = New(Options{
		BaseURL:   srv.URL,
		HTTP:      srv.Client(),
		Now:       func() time.Time { return testNow },
		LookPath:  func(string) (string, error) { return "/usr/local/bin/opencode", nil },
		UserAgent: fakeUA,
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			f.runName, f.runArgs = name, args
			return []byte("opencode-go/glm-5\n\n  opencode-go/kimi-k2.6  \nopencode-go/qwen3.7-plus\n"), nil
		},
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
	want := []struct {
		label string
		len   time.Duration
		pct   float64
		reset string
	}{
		{"rolling", 5 * time.Hour, 0, "2026-10-23T03:55:21.822Z"},
		{"weekly", 7 * 24 * time.Hour, 5, "2026-10-26T00:00:00Z"},
		{"monthly", 30 * 24 * time.Hour, 95, "2026-10-24T14:17:51Z"},
	}
	if len(r.Windows) != len(want) {
		t.Fatalf("windows = %+v", r.Windows)
	}
	for i, w := range want {
		got := r.Windows[i]
		rt, _ := time.Parse(time.RFC3339Nano, w.reset)
		if got.Label != w.label || got.Length != w.len || got.UsedPct != w.pct || !got.ResetsAt.Equal(rt) {
			t.Errorf("window %d = %+v, want %+v", i, got, w)
		}
	}
	if r.Windows[2].ResetsAt.Format(time.RFC3339) != "2026-10-24T14:17:51Z" {
		t.Errorf("monthly reset = %v", r.Windows[2].ResetsAt)
	}
	if len(r.Notes) != 0 || !r.TakenAt.Equal(testNow) {
		t.Fatalf("notes = %v, TakenAt = %v", r.Notes, r.TakenAt)
	}
	req := f.lastReq
	if req.Method != "GET" || req.URL.Path != "/zen/go/v1/usage" {
		t.Fatalf("request = %s %s", req.Method, req.URL.Path)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer "+fakeKey2 {
		t.Errorf("Authorization = %q", got)
	}
	if got := req.Header.Get("User-Agent"); got != fakeUA {
		t.Errorf("User-Agent = %q", got)
	}
	if f.hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", f.hits.Load())
	}
}

func TestRateLimitedMeansFull(t *testing.T) {
	body := `{"usage":{"rolling":{"status":"rate-limited","percent":40,"resetsAt":"2026-10-23T03:55:21.822Z"},
	"weekly":{"status":"ok","percent":5,"resetsAt":"2026-10-26T00:00:00.000Z"}}}`
	f := newFixture(t, serveBody(200, body))
	r, err := f.a.Usage(context.Background(), f.acct)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Windows) != 2 || r.Windows[0].UsedPct != 100 || r.Windows[1].UsedPct != 5 {
		t.Fatalf("windows = %+v", r.Windows)
	}
}

func TestUnknownStatusKeepsPercentAndNotes(t *testing.T) {
	body := `{"usage":{"weekly":{"status":"degraded","percent":12,"resetsAt":"2026-10-26T00:00:00.000Z"}}}`
	f := newFixture(t, serveBody(200, body))
	r, err := f.a.Usage(context.Background(), f.acct)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Windows) != 1 || r.Windows[0].UsedPct != 12 {
		t.Fatalf("windows = %+v", r.Windows)
	}
	if want := []string{`unrecognised status "degraded" on weekly`}; !reflect.DeepEqual(r.Notes, want) {
		t.Fatalf("notes = %q, want %q", r.Notes, want)
	}
}

func TestUsageShapeChange(t *testing.T) {
	for name, body := range map[string]string{
		"no usage":       `{"other":{}}`,
		"not json":       `<html>`,
		"usage null":     `{"usage":null}`,
		"no windows":     `{"usage":{}}`,
		"no percent":     `{"usage":{"rolling":{"status":"ok","resetsAt":"2026-10-23T03:55:21.822Z"}}}`,
		"bad reset":      `{"usage":{"rolling":{"status":"ok","percent":1,"resetsAt":"soon"}}}`,
		"percent string": `{"usage":{"rolling":{"status":"ok","percent":"1"}}}`,
		"window not obj": `{"usage":{"rolling":3}}`,
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

func TestUsageHTTPErrors(t *testing.T) {
	f := newFixture(t, serveBody(401, `{}`))
	if _, err := f.a.Usage(context.Background(), f.acct); !errors.Is(err, adapter.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	for _, tc := range []struct {
		status int
		retry  string
		want   time.Duration
	}{
		{429, "120", 120 * time.Second},
		{429, "Wed, 21 Oct 2026 07:28:00 GMT", 0},
		{429, "", 0},
		{403, "30", 0},
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

func TestUsageWithoutKeyMakesNoRequest(t *testing.T) {
	f := newFixture(t, serveBody(200, "{}"))
	f.acct.KeyLine = 9
	if _, err := f.a.Usage(context.Background(), f.acct); err == nil {
		t.Fatal("want an error")
	}
	if f.hits.Load() != 0 {
		t.Fatalf("hits = %d, want 0", f.hits.Load())
	}
}

func TestKeyAt(t *testing.T) {
	p := writeKeys(t)
	for line, want := range map[int]string{1: fakeKey1, 2: fakeKey2, 3: fakeKey3} {
		k, found, err := KeyAt(p, line)
		if err != nil || !found || k != want {
			t.Errorf("line %d: key=%q found=%v err=%v", line, k, found, err)
		}
	}
	for _, line := range []int{4, 0, -1} {
		k, found, err := KeyAt(p, line)
		if err != nil || found || k != "" {
			t.Errorf("line %d: key=%q found=%v err=%v, want not found", line, k, found, err)
		}
	}
	if _, found, err := KeyAt(filepath.Join(t.TempDir(), "absent"), 1); found || !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing file: found=%v err=%v", found, err)
	}
}

func TestCredential(t *testing.T) {
	f := newFixture(t, serveBody(200, "{}"))
	st := f.a.Credential(f.acct)
	if !st.Present || st.Expired || st.Err != nil {
		t.Fatalf("status = %+v", st)
	}
	if want := fmt.Sprintf("key file %s line 2", f.acct.KeyFile); st.Source != want {
		t.Fatalf("source = %q, want %q", st.Source, want)
	}
	f.acct.KeyLine = 4
	if st := f.a.Credential(f.acct); st.Present || st.Expired || st.Err != nil || !strings.Contains(st.Source, "line 4") {
		t.Fatalf("missing line: %+v", st)
	}
	f.acct.KeyFile = filepath.Join(t.TempDir(), "absent")
	if st := f.a.Credential(f.acct); st.Present || st.Expired {
		t.Fatalf("missing file: %+v", st)
	}
	f.acct.KeyFile = ""
	if st := f.a.Credential(f.acct); st.Present || st.Source == "" {
		t.Fatalf("no key_file: %+v", st)
	}
}

func TestModelsFromRunner(t *testing.T) {
	f := newFixture(t, serveBody(200, "{}"))
	got, err := f.a.Models(context.Background(), f.acct)
	if err != nil {
		t.Fatal(err)
	}
	want := []adapter.Model{{ID: "opencode-go/glm-5"}, {ID: "opencode-go/kimi-k2.6"}, {ID: "opencode-go/qwen3.7-plus"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("models = %+v, want %+v", got, want)
	}
	if f.runName != "opencode" || !reflect.DeepEqual(f.runArgs, []string{"models", "opencode-go"}) {
		t.Fatalf("ran %s %v", f.runName, f.runArgs)
	}
	f.a.o.Run = func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("boom") }
	if _, err := f.a.Models(context.Background(), f.acct); err == nil {
		t.Fatal("want runner error")
	}
}

func TestCheck(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		f := newFixture(t, serveBody(200, "{}"))
		if got := f.a.Check(context.Background(), f.acct); len(got) != 0 {
			t.Fatalf("findings = %+v", got)
		}
	})
	t.Run("no binary, no line", func(t *testing.T) {
		f := newFixture(t, serveBody(200, "{}"))
		f.a.o.LookPath = func(string) (string, error) { return "", errors.New("nope") }
		f.acct.KeyLine = 7
		got := f.a.Check(context.Background(), f.acct)
		if len(got) != 2 || got[0].Severity != adapter.SeverityError || got[1].Severity != adapter.SeverityError || got[1].Account != "go-2" {
			t.Fatalf("findings = %+v", got)
		}
	})
}

func TestVendor(t *testing.T) {
	if v := New(Options{}).Vendor(); v != "opencode-go" {
		t.Fatal(v)
	}
}

func TestErrorsNeverContainToken(t *testing.T) {
	secrets := []string{fakeKey1, fakeKey2, fakeKey3}
	echo := func(status int) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(status)
			_, _ = fmt.Fprintf(w, `{"echo":%q}`, r.Header.Get("Authorization"))
		}
	}
	probes := []struct {
		name    string
		handler http.HandlerFunc
		dead    bool
		line    int
	}{
		{name: "401", handler: echo(401)},
		{name: "403", handler: echo(403)},
		{name: "429", handler: echo(429)},
		{name: "500", handler: echo(500)},
		{name: "shape", handler: echo(200)},
		{name: "not json", handler: serveBody(200, "garbage "+fakeKey2)},
		{name: "network", handler: serveBody(200, "{}"), dead: true},
		{name: "no line", handler: serveBody(200, "{}"), line: 9},
	}
	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			f := newFixture(t, p.handler)
			if p.line != 0 {
				f.acct.KeyLine = p.line
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
