package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/core"
)

// now is the fixed clock of every test; readings reset well after it.
var (
	now   = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	later = now.Add(72 * time.Hour)
)

// fakeAdapter serves canned usage per account name and counts calls. An
// account missing from readings and errs has unknown usage (ErrShape).
type fakeAdapter struct {
	vendor    string
	readings  map[string]adapter.Reading
	errs      map[string]error
	usage     map[string]int // Usage calls per account
	credCalls map[string]int
}

func newFake(vendor string) *fakeAdapter {
	return &fakeAdapter{
		vendor:    vendor,
		readings:  map[string]adapter.Reading{},
		errs:      map[string]error{},
		usage:     map[string]int{},
		credCalls: map[string]int{},
	}
}

func (f *fakeAdapter) Vendor() string { return f.vendor }

func (f *fakeAdapter) Credential(acct adapter.Account) adapter.CredStatus {
	f.credCalls[acct.Name]++
	return adapter.CredStatus{Present: true, Source: "fake: " + acct.Name}
}

func (f *fakeAdapter) Usage(_ context.Context, acct adapter.Account) (adapter.Reading, error) {
	f.usage[acct.Name]++
	if err, ok := f.errs[acct.Name]; ok {
		return adapter.Reading{}, err
	}
	if r, ok := f.readings[acct.Name]; ok {
		return r, nil
	}
	return adapter.Reading{}, adapter.ErrShape
}

func (f *fakeAdapter) Models(context.Context, adapter.Account) ([]adapter.Model, error) {
	return nil, errors.New("fake: no models")
}

func (f *fakeAdapter) Check(context.Context, adapter.Account) []adapter.Finding { return nil }

func (f *fakeAdapter) totalUsage() int {
	n := 0
	for _, c := range f.usage {
		n += c
	}
	return n
}

// claudeReading reports a 5h and a 7d window, matching a two-value cap so no
// cap-length warning is produced.
func claudeReading(session, weekly float64) adapter.Reading {
	return adapter.Reading{TakenAt: now, Windows: []adapter.Window{
		{Label: "session", Length: 5 * time.Hour, UsedPct: session, ResetsAt: later},
		{Label: "weekly", Length: 7 * 24 * time.Hour, UsedPct: weekly, ResetsAt: later},
	}}
}

const testConfig = `{
  "version": 1,
  "accounts": {
    "claude-main": { "vendor": "claude", "config_dir": "~/.claude" },
    "claude-rec":  { "vendor": "claude", "config_dir": "~/.claude-rec" },
    "codex-main":  { "vendor": "codex", "codex_home": "~/.codex" }
  },
  "profiles": {
    "two-claude": [
      { "account": "claude-main", "model": "claude-opus-5-5", "thinking": "high", "cap": [90, 98] },
      { "account": "claude-rec", "model": "claude-sonnet-5-5", "thinking": "medium" }
    ],
    "codex-first": [
      { "account": "codex-main", "model": "gpt-6.1-sol", "thinking": "high", "cap": [98] },
      { "account": "claude-rec", "model": "claude-sonnet-5-5", "thinking": "medium" }
    ],
    "same-account": [
      { "account": "claude-main", "model": "claude-opus-5-5", "thinking": "high", "cap": [90, 98] },
      { "account": "claude-main", "model": "claude-fable-5-1", "thinking": "high", "cap": [90, 98] },
      { "account": "claude-main", "model": "claude-sonnet-5-5", "thinking": "medium", "cap": [90, 98] }
    ]
  }
}`

// testDeps writes configJSON to a temp file and points MODELPICK_CONFIG and
// XDG_CACHE_HOME at temp paths, so nothing real is read or written.
func testDeps(t *testing.T, configJSON string, fakes ...*fakeAdapter) Deps {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{
		"MODELPICK_CONFIG": cfgPath,
		"XDG_CACHE_HOME":   filepath.Join(dir, "cache"),
	}
	adapters := map[string]adapter.Adapter{}
	for _, f := range fakes {
		adapters[f.vendor] = f
	}
	return Deps{
		Getenv:   func(k string) string { return vars[k] },
		Home:     "/home/u",
		Now:      func() time.Time { return now },
		Adapters: adapters,
	}
}

func run(d Deps, args ...string) (stdout, stderr string, code int) {
	var out, errOut bytes.Buffer
	code = Main(args, &out, &errOut, d)
	return out.String(), errOut.String(), code
}

// pickOutput is the parsed pick JSON.
type pickOutput struct {
	Status     string          `json:"status"`
	Profile    string          `json:"profile"`
	Vendor     *string         `json:"vendor"`
	Account    *string         `json:"account"`
	Model      *string         `json:"model"`
	Thinking   *string         `json:"thinking"`
	AccountRef map[string]any  `json:"account_ref"`
	Reason     *string         `json:"reason"`
	Skipped    []core.Skip     `json:"skipped"`
	Errors     []core.RunError `json:"errors"`
	Warnings   []string        `json:"warnings"`
}

func parsePick(t *testing.T, stdout string) pickOutput {
	t.Helper()
	var p pickOutput
	if err := json.Unmarshal([]byte(stdout), &p); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	return p
}

func TestPickEndToEnd(t *testing.T) {
	claude := newFake("claude")
	claude.readings["claude-main"] = claudeReading(92, 10)
	d := testDeps(t, testConfig, claude, newFake("codex"))

	stdout, stderr, code := run(d, "pick", "two-claude")
	if code != 0 {
		t.Fatalf("exit %d, want 0; stderr %q; stdout\n%s", code, stderr, stdout)
	}
	p := parsePick(t, stdout)
	if p.Status != "ok" || p.Account == nil || *p.Account != "claude-rec" {
		t.Fatalf("want ok/claude-rec, got\n%s", stdout)
	}
	if *p.Vendor != "claude" || *p.Model != "claude-sonnet-5-5" || *p.Thinking != "medium" || p.Reason != nil {
		t.Fatalf("wrong pick fields:\n%s", stdout)
	}
	if got := p.AccountRef["config_dir"]; got != "/home/u/.claude-rec" {
		t.Fatalf("account_ref.config_dir = %v, want /home/u/.claude-rec", got)
	}
	wantSkip := []core.Skip{{Account: "claude-main", Model: "claude-opus-5-5", Why: "5h window 92% >= cap 90"}}
	if len(p.Skipped) != 1 || p.Skipped[0] != wantSkip[0] {
		t.Fatalf("skipped = %+v, want %+v", p.Skipped, wantSkip)
	}
	if len(p.Errors) != 0 || len(p.Warnings) != 0 {
		t.Fatalf("want no errors/warnings, got %+v / %q", p.Errors, p.Warnings)
	}
	if claude.usage["claude-main"] != 1 || claude.usage["claude-rec"] != 0 {
		t.Fatalf("usage calls = %v, want claude-main 1 and claude-rec 0 (uncapped)", claude.usage)
	}

	keys := []string{"status", "profile", "vendor", "account", "model", "thinking",
		"account_ref", "reason", "skipped", "errors", "warnings"}
	prev := -1
	for _, k := range keys {
		// Top-level keys are the only ones at a 2-space indent.
		i := strings.Index(stdout, "\n  \""+k+"\": ")
		if i < 0 {
			t.Fatalf("top-level key %q missing:\n%s", k, stdout)
		}
		if i <= prev {
			t.Fatalf("key %q out of order:\n%s", k, stdout)
		}
		prev = i
	}
	if n := strings.Count(stdout, "\n  \""); n != len(keys) {
		t.Fatalf("%d top-level keys, want %d:\n%s", n, len(keys), stdout)
	}
}

func TestPickConfigError(t *testing.T) {
	bad := strings.Replace(testConfig,
		`"cap": [90, 98] },
      { "account": "claude-rec"`,
		`"cap": [90, 98], "skip_during": ["deepseek-peek"] },
      { "account": "claude-rec"`, 1)
	if bad == testConfig {
		t.Fatal("test setup: replacement did not apply")
	}
	claude := newFake("claude")
	d := testDeps(t, bad, claude, newFake("codex"))

	stdout, _, code := run(d, "pick", "two-claude")
	if code != 1 {
		t.Fatalf("exit %d, want 1:\n%s", code, stdout)
	}
	p := parsePick(t, stdout)
	if p.Status != "failed" || p.Profile != "two-claude" || p.Reason == nil {
		t.Fatalf("want failed with a reason:\n%s", stdout)
	}
	if !strings.HasPrefix(*p.Reason, "config ") || !strings.Contains(*p.Reason, `unknown schedule "deepseek-peek"`) {
		t.Fatalf("reason = %q, want config …unknown schedule", *p.Reason)
	}
	if p.Account != nil || p.AccountRef != nil || p.Skipped == nil || p.Errors == nil || p.Warnings == nil {
		t.Fatalf("failed result must have null pick fields and [] lists:\n%s", stdout)
	}
	if claude.totalUsage() != 0 {
		t.Fatalf("config error made %d usage calls", claude.totalUsage())
	}
}

func TestPickMissingConfigFile(t *testing.T) {
	d := testDeps(t, testConfig)
	d.Getenv = func(k string) string {
		if k == "MODELPICK_CONFIG" {
			return filepath.Join(t.TempDir(), "absent.json")
		}
		return ""
	}
	stdout, _, code := run(d, "pick", "two-claude")
	p := parsePick(t, stdout)
	if code != 1 || p.Status != "failed" || p.Reason == nil || !strings.Contains(*p.Reason, "absent.json") {
		t.Fatalf("exit %d, want 1 and a reason naming the file:\n%s", code, stdout)
	}
}

func TestPickUnauthorizedIsOkWithErrors(t *testing.T) {
	codex := newFake("codex")
	codex.errs["codex-main"] = errors.Join(errors.New("codex"), adapter.ErrUnauthorized)
	claude := newFake("claude")
	d := testDeps(t, testConfig, claude, codex)

	stdout, _, code := run(d, "pick", "codex-first")
	if code != 2 {
		t.Fatalf("exit %d, want 2:\n%s", code, stdout)
	}
	p := parsePick(t, stdout)
	if p.Status != "ok_with_errors" || p.Account == nil || *p.Account != "claude-rec" {
		t.Fatalf("want ok_with_errors/claude-rec:\n%s", stdout)
	}
	want := core.RunError{Account: "codex-main", Error: "usage endpoint returned HTTP 401; log in to Codex with this account"}
	if len(p.Errors) != 1 || p.Errors[0] != want {
		t.Fatalf("errors = %+v, want [%+v]", p.Errors, want)
	}
	if len(p.Skipped) != 1 || p.Skipped[0].Why != "usage unknown (see errors)" {
		t.Fatalf("skipped = %+v", p.Skipped)
	}
}

func TestEnvReadsEachAccountOnce(t *testing.T) {
	claude := newFake("claude")
	claude.readings["claude-main"] = claudeReading(95, 10)
	d := testDeps(t, testConfig, claude, newFake("codex"))

	stdout, _, code := run(d, "pick", "same-account")
	p := parsePick(t, stdout)
	if code != 1 || p.Status != "failed" || len(p.Skipped) != 3 {
		t.Fatalf("exit %d, want 1 with 3 skips:\n%s", code, stdout)
	}
	if got := claude.usage["claude-main"]; got != 1 {
		t.Fatalf("Usage called %d times for claude-main, want 1", got)
	}
}

// TestEnvMemoisesPerAccount checks the env's own memo, with a zero floor so
// neither the store nor core.Pick could be what prevents the second call.
func TestEnvMemoisesPerAccount(t *testing.T) {
	cfgJSON := strings.Replace(testConfig, `"version": 1,`,
		`"version": 1, "settings": { "min_call_interval_minutes": 0 },`, 1)
	claude := newFake("claude")
	claude.readings["claude-main"] = claudeReading(10, 10)
	d := testDeps(t, cfgJSON, claude)
	cfg, err := core.Load(d.Getenv("MODELPICK_CONFIG"), d.Home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Settings.MinCallInterval != 0 {
		t.Fatalf("test setup: floor = %v, want 0", cfg.Settings.MinCallInterval)
	}
	e := newEnv(d, cfg, false)

	for range 2 {
		if c := e.Credential("claude-main"); !c.Present {
			t.Fatalf("credential = %+v", c)
		}
		if st := e.Usage("claude-main"); st.Reading == nil || st.Err != nil {
			t.Fatalf("usage = %+v", st)
		}
	}
	if got := claude.usage["claude-main"]; got != 1 {
		t.Fatalf("Usage called %d times, want 1", got)
	}
	// The store asks the adapter once more itself (expiry check), so two
	// calls: one from the env's memo, one from the single Get.
	if got := claude.credCalls["claude-main"]; got != 2 {
		t.Fatalf("Credential called %d times, want 2 (env once, store once)", got)
	}

	// A second env (a second pick) does call again: the floor is zero.
	newEnv(d, cfg, false).Usage("claude-main")
	if got := claude.usage["claude-main"]; got != 2 {
		t.Fatalf("a new env made %d total calls, want 2", got)
	}
}

func TestPickRefreshFlag(t *testing.T) {
	claude := newFake("claude")
	claude.readings["claude-main"] = claudeReading(10, 10)
	d := testDeps(t, testConfig, claude)
	minute := now
	d.Now = func() time.Time { return minute }

	pickOnce := func(args ...string) {
		t.Helper()
		stdout, _, code := run(d, append([]string{"pick", "two-claude"}, args...)...)
		p := parsePick(t, stdout)
		if code != 0 || p.Account == nil || *p.Account != "claude-main" {
			t.Fatalf("exit %d, want 0 picking claude-main:\n%s", code, stdout)
		}
	}
	pickOnce()
	if got := claude.usage["claude-main"]; got != 1 {
		t.Fatalf("first pick: %d calls, want 1", got)
	}
	minute = now.Add(5 * time.Minute) // within the 15-minute default floor
	pickOnce()
	if got := claude.usage["claude-main"]; got != 1 {
		t.Fatalf("second pick within the floor: %d calls, want still 1", got)
	}
	pickOnce("--refresh")
	if got := claude.usage["claude-main"]; got != 2 {
		t.Fatalf("pick --refresh: %d calls, want 2", got)
	}
}

func TestUsageErrorMessages(t *testing.T) {
	wrapped := func(e error) error { return errors.Join(errors.New("vendor"), e) }
	cases := []struct {
		vendor string
		err    error
		want   string
	}{
		{"claude", wrapped(adapter.ErrUnauthorized), "usage endpoint returned HTTP 401; log in to Claude Code with this account"},
		{"codex", wrapped(adapter.ErrUnauthorized), "usage endpoint returned HTTP 401; log in to Codex with this account"},
		{"opencode-go", wrapped(adapter.ErrUnauthorized), "usage endpoint returned HTTP 401; log in to OpenCode with this account"},
		{"claude", adapter.ErrTokenExpired, "token expired; open Claude Code with this account to refresh it"},
		{"codex", adapter.ErrTokenExpired, "token expired; open Codex with this account to refresh it"},
		{"claude", &adapter.HTTPError{Status: 503}, "usage endpoint returned HTTP 503"},
	}
	for _, c := range cases {
		got := usageError(c.vendor, c.err)
		if got.Error() != c.want {
			t.Errorf("%s %v: got %q, want %q", c.vendor, c.err, got.Error(), c.want)
		}
		if !errors.Is(got, c.err) {
			t.Errorf("%s %v: rewritten error no longer matches the original", c.vendor, c.err)
		}
	}
}

func TestUsageErrors(t *testing.T) {
	cases := [][]string{
		{},
		{"pick"},
		{"pick", "a", "b"},
		{"pick", "two-claude", "--refrsh"},
		{"bogus"},
	}
	for _, args := range cases {
		claude := newFake("claude")
		stdout, stderr, code := run(testDeps(t, testConfig, claude), args...)
		if code != 1 || stdout != "" || !strings.Contains(stderr, "usage:") {
			t.Errorf("%q: exit %d stdout %q stderr %q; want 1, empty stdout, usage on stderr", args, code, stdout, stderr)
		}
		if claude.totalUsage() != 0 {
			t.Errorf("%q: made usage calls", args)
		}
	}
}

func TestPickRefreshBeforeProfile(t *testing.T) {
	claude := newFake("claude")
	claude.readings["claude-main"] = claudeReading(10, 10)
	stdout, _, code := run(testDeps(t, testConfig, claude), "pick", "--refresh", "two-claude")
	if p := parsePick(t, stdout); code != 0 || p.Profile != "two-claude" {
		t.Fatalf("exit %d:\n%s", code, stdout)
	}
}

func TestPickWithoutHomeFails(t *testing.T) {
	claude := newFake("claude")
	d := testDeps(t, testConfig, claude)
	d.Home = ""
	stdout, _, code := run(d, "pick", "two-claude")
	p := parsePick(t, stdout)
	if code != 1 || p.Status != "failed" || p.Reason == nil || !strings.Contains(*p.Reason, "home directory") {
		t.Fatalf("exit %d, want 1 failed naming the home directory:\n%s", code, stdout)
	}
	if claude.totalUsage() != 0 {
		t.Fatal("made usage calls without a home directory")
	}
}

func TestDefaultDepsAdaptersKeyedByVendor(t *testing.T) {
	d := DefaultDeps()
	want := []string{"claude", "codex", "opencode-go"}
	if len(d.Adapters) != len(want) {
		t.Fatalf("adapters = %v, want %v", d.Adapters, want)
	}
	for _, v := range want {
		a, ok := d.Adapters[v]
		if !ok || a.Vendor() != v {
			t.Fatalf("Adapters[%q] = %v", v, a)
		}
	}
	if d.Getenv == nil || d.Now == nil {
		t.Fatal("DefaultDeps left Getenv or Now nil")
	}
}

func TestHTTPClientDoesNotFollowRedirects(t *testing.T) {
	var reached int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached++ }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer origin.Close()

	c := newHTTPClient()
	if c.Timeout != 10*time.Second {
		t.Fatalf("timeout = %v, want 10s", c.Timeout)
	}
	resp, err := c.Get(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || reached != 0 {
		t.Fatalf("status %d, redirect target reached %d times; want 302 and 0", resp.StatusCode, reached)
	}
}
