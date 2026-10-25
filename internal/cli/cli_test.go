package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/app/apptest"
	"github.com/meatlover/modelpick/internal/core"
)

const testConfig = apptest.Config

func run(d app.Deps, args ...string) (stdout, stderr string, code int) {
	var out, errOut bytes.Buffer
	code = Main(args, &out, &errOut, d)
	return out.String(), errOut.String(), code
}

// pickOutput is the parsed pick JSON.
type pickOutput struct {
	Status      string          `json:"status"`
	Profile     string          `json:"profile"`
	Vendor      *string         `json:"vendor"`
	Account     *string         `json:"account"`
	Model       *string         `json:"model"`
	Thinking    *string         `json:"thinking"`
	AccountRef  map[string]any  `json:"account_ref"`
	LoadBalance *core.LBPick    `json:"load_balance"`
	Reason      *string         `json:"reason"`
	Skipped     []core.Skip     `json:"skipped"`
	Errors      []core.RunError `json:"errors"`
	Warnings    []string        `json:"warnings"`
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
	claude := apptest.NewFake("claude")
	claude.Readings["claude-main"] = apptest.ClaudeReading(92, 10)
	d := apptest.NewDeps(t, testConfig, claude, apptest.NewFake("codex"))

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
	if claude.Calls["claude-main"] != 1 || claude.Calls["claude-rec"] != 0 {
		t.Fatalf("usage calls = %v, want claude-main 1 and claude-rec 0 (uncapped)", claude.Calls)
	}

	keys := []string{"status", "profile", "vendor", "account", "model", "thinking",
		"account_ref", "load_balance", "reason", "skipped", "errors", "warnings"}
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

// lbConfig has profile lb: claude-main and claude-rec in one group.
const lbConfig = `{
  "version": 1,
  "accounts": {
    "claude-main": { "vendor": "claude", "config_dir": "~/.claude" },
    "claude-rec":  { "vendor": "claude", "config_dir": "~/.claude-rec" }
  },
  "profiles": {
    "lb": [
      { "load_balance": "random", "choices": [
        { "account": "claude-main", "model": "claude-sonnet-5-5", "thinking": "medium" },
        { "account": "claude-rec", "model": "claude-sonnet-5-5", "thinking": "medium" }
      ] }
    ]
  }
}`

// TestPickCLIUsesDepsShuffle: the group's random source is Deps.Shuffle.
func TestPickCLIUsesDepsShuffle(t *testing.T) {
	reverse := func(n int, swap func(i, j int)) {
		for i := 0; i < n/2; i++ {
			swap(i, n-1-i)
		}
	}
	cases := []struct {
		name    string
		shuffle func(int, func(i, j int))
		account string
		member  string
	}{
		{"fake deps keep file order", nil, "claude-main", "1a"},
		{"reversed", reverse, "claude-rec", "1b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := apptest.NewDeps(t, lbConfig, apptest.NewFake("claude"))
			if tc.shuffle != nil {
				d.Shuffle = tc.shuffle
			}
			stdout, _, code := run(d, "pick", "lb")
			p := parsePick(t, stdout)
			if code != 0 || p.Account == nil || *p.Account != tc.account {
				t.Fatalf("exit %d, want 0 and account %s:\n%s", code, tc.account, stdout)
			}
			want := core.LBPick{Algorithm: "random", Choice: 1, Member: tc.member}
			if p.LoadBalance == nil || *p.LoadBalance != want {
				t.Fatalf("load_balance = %+v, want %+v:\n%s", p.LoadBalance, want, stdout)
			}
		})
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
	claude := apptest.NewFake("claude")
	d := apptest.NewDeps(t, bad, claude, apptest.NewFake("codex"))

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
	if claude.TotalUsage() != 0 {
		t.Fatalf("config error made %d usage calls", claude.TotalUsage())
	}
}

func TestPickMissingConfigFile(t *testing.T) {
	d := apptest.NewDeps(t, testConfig)
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
	codex := apptest.NewFake("codex")
	codex.Errs["codex-main"] = errors.Join(errors.New("codex"), adapter.ErrUnauthorized)
	claude := apptest.NewFake("claude")
	d := apptest.NewDeps(t, testConfig, claude, codex)

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
	claude := apptest.NewFake("claude")
	claude.Readings["claude-main"] = apptest.ClaudeReading(95, 10)
	d := apptest.NewDeps(t, testConfig, claude, apptest.NewFake("codex"))

	stdout, _, code := run(d, "pick", "same-account")
	p := parsePick(t, stdout)
	if code != 1 || p.Status != "failed" || len(p.Skipped) != 3 {
		t.Fatalf("exit %d, want 1 with 3 skips:\n%s", code, stdout)
	}
	if got := claude.Calls["claude-main"]; got != 1 {
		t.Fatalf("Usage called %d times for claude-main, want 1", got)
	}
}

func TestPickRefreshFlag(t *testing.T) {
	claude := apptest.NewFake("claude")
	claude.Readings["claude-main"] = apptest.ClaudeReading(10, 10)
	d := apptest.NewDeps(t, testConfig, claude)
	minute := apptest.Now
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
	if got := claude.Calls["claude-main"]; got != 1 {
		t.Fatalf("first pick: %d calls, want 1", got)
	}
	minute = apptest.Now.Add(5 * time.Minute) // within the 15-minute default floor
	pickOnce()
	if got := claude.Calls["claude-main"]; got != 1 {
		t.Fatalf("second pick within the floor: %d calls, want still 1", got)
	}
	pickOnce("--refresh")
	if got := claude.Calls["claude-main"]; got != 2 {
		t.Fatalf("pick --refresh: %d calls, want 2", got)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{{}, {"bogus"}} {
		claude := apptest.NewFake("claude")
		stdout, stderr, code := run(apptest.NewDeps(t, testConfig, claude), args...)
		if code != 1 || stdout != "" || !strings.Contains(stderr, "usage:") {
			t.Errorf("%q: exit %d stdout %q stderr %q; want 1, empty stdout, usage on stderr", args, code, stdout, stderr)
		}
		if claude.TotalUsage() != 0 {
			t.Errorf("%q: made usage calls", args)
		}
	}
}

// TestPickUsageErrorsPrintFailedJSON: a launcher reads stdout, so a bad pick
// command line still yields a failed result there, with usage on stderr.
func TestPickUsageErrorsPrintFailedJSON(t *testing.T) {
	for _, c := range []struct {
		args    []string
		profile string
		reason  string
	}{
		{[]string{"pick", ""}, "", "pick: missing <profile>"},
		{[]string{"pick", "", "--refresh"}, "", "pick: missing <profile>"},
		{[]string{"pick", "a", "b"}, "a", `pick: unexpected argument "b"`},
		{[]string{"pick", "two-claude", "--refrsh"}, "two-claude", `pick: unknown flag "--refrsh"`},
	} {
		claude := apptest.NewFake("claude")
		stdout, stderr, code := run(apptest.NewDeps(t, testConfig, claude), c.args...)
		if code != 1 || !strings.Contains(stderr, "usage:") {
			t.Errorf("%q: exit %d stderr %q; want 1 with usage on stderr", c.args, code, stderr)
			continue
		}
		p := parsePick(t, stdout)
		if p.Status != "failed" || p.Profile != c.profile || p.Reason == nil || *p.Reason != c.reason ||
			p.Skipped == nil || p.Errors == nil || p.Warnings == nil {
			t.Errorf("%q: got %+v, want failed, profile %q, reason %q", c.args, p, c.profile, c.reason)
		}
		if claude.TotalUsage() != 0 {
			t.Errorf("%q: made usage calls", c.args)
		}
	}
}

func TestPickRefreshBeforeProfile(t *testing.T) {
	claude := apptest.NewFake("claude")
	claude.Readings["claude-main"] = apptest.ClaudeReading(10, 10)
	stdout, _, code := run(apptest.NewDeps(t, testConfig, claude), "pick", "--refresh", "two-claude")
	if p := parsePick(t, stdout); code != 0 || p.Profile != "two-claude" {
		t.Fatalf("exit %d:\n%s", code, stdout)
	}
}

func TestPickWithoutHomeFails(t *testing.T) {
	claude := apptest.NewFake("claude")
	d := apptest.NewDeps(t, testConfig, claude)
	d.Home = ""
	stdout, _, code := run(d, "pick", "two-claude")
	p := parsePick(t, stdout)
	if code != 1 || p.Status != "failed" || p.Reason == nil || !strings.Contains(*p.Reason, "home directory") {
		t.Fatalf("exit %d, want 1 failed naming the home directory:\n%s", code, stdout)
	}
	if claude.TotalUsage() != 0 {
		t.Fatal("made usage calls without a home directory")
	}
}
