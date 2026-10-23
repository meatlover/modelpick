package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/app/apptest"
)

// lsFakes: claude-main ok, claude-rec unauthorized, codex-main ok.
func lsFakes() (*apptest.Fake, *apptest.Fake) {
	claude := apptest.NewFake("claude")
	claude.Readings["claude-main"] = apptest.ClaudeReading(40, 10)
	claude.Errs["claude-rec"] = adapter.ErrUnauthorized
	codex := apptest.NewFake("codex")
	codex.Readings["codex-main"] = apptest.ClaudeReading(5, 6)
	return claude, codex
}

func TestLsTableHasEveryAccount(t *testing.T) {
	claude, codex := lsFakes()
	d := apptest.NewDeps(t, testConfig, claude, codex)
	stdout, stderr, code := run(d, "ls")
	if code != 0 {
		t.Fatalf("exit %d, want 0; stderr %q", code, stderr)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if got := strings.Fields(lines[0]); strings.Join(got, " ") != "ACCOUNT VENDOR WINDOW USED RESETS AGE STATE" {
		t.Fatalf("header = %q", lines[0])
	}
	for _, name := range []string{"claude-main", "claude-rec", "codex-main"} {
		if !strings.Contains(stdout, name) {
			t.Errorf("account %s missing:\n%s", name, stdout)
		}
	}
	var recRow string
	for _, l := range lines {
		if strings.HasPrefix(l, "claude-rec") {
			recRow = l
		}
	}
	if !strings.Contains(recRow, "error: usage endpoint returned HTTP 401; log in to Claude Code") {
		t.Errorf("claude-rec row = %q", recRow)
	}
	// two windows for claude-main → two rows, the ok state on both.
	if n := strings.Count(stdout, "claude-main"); n != 2 {
		t.Errorf("claude-main rows = %d, want 2:\n%s", n, stdout)
	}
	if !strings.Contains(stdout, "40%") || !strings.Contains(stdout, "session (5h)") {
		t.Errorf("window columns missing:\n%s", stdout)
	}
}

type gotWindow struct {
	Label    string  `json:"label"`
	Length   string  `json:"length"`
	UsedPct  float64 `json:"used_pct"`
	ResetsAt string  `json:"resets_at"`
	Scope    *string `json:"scope"`
}

type gotAccount struct {
	Account    string      `json:"account"`
	Vendor     string      `json:"vendor"`
	State      string      `json:"state"`
	AgeSeconds *int        `json:"reading_age_seconds"`
	Windows    []gotWindow `json:"windows"`
}

func TestLsJSONShape(t *testing.T) {
	claude, codex := lsFakes()
	d := apptest.NewDeps(t, testConfig, claude, codex)
	stdout, _, code := run(d, "ls", "--json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var got []gotAccount
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	if len(got) != 3 || got[0].Account != "claude-main" || got[1].Account != "claude-rec" || got[2].Account != "codex-main" {
		t.Fatalf("accounts not sorted by name: %+v", got)
	}
	main, rec := got[0], got[1]
	if main.State != "ok" || main.Vendor != "claude" || main.AgeSeconds == nil || *main.AgeSeconds != 0 {
		t.Errorf("claude-main = %+v", main)
	}
	if len(main.Windows) != 2 || main.Windows[0].Length != "5h" || main.Windows[1].Length != "7d" ||
		main.Windows[0].UsedPct != 40 || main.Windows[0].Scope != nil ||
		main.Windows[0].ResetsAt != apptest.Later.Format(time.RFC3339) {
		t.Errorf("claude-main windows = %+v", main.Windows)
	}
	if rec.AgeSeconds != nil || rec.Windows == nil || len(rec.Windows) != 0 || !strings.HasPrefix(rec.State, "error: ") {
		t.Errorf("claude-rec = %+v", rec)
	}
	if !strings.Contains(stdout, `"reading_age_seconds": null`) {
		t.Errorf("reading_age_seconds must be an explicit null:\n%s", stdout)
	}
}

func TestLsRespectsFloor(t *testing.T) {
	claude, codex := lsFakes()
	d := apptest.NewDeps(t, testConfig, claude, codex)
	for i := 0; i < 2; i++ {
		if _, _, code := run(d, "ls"); code != 0 {
			t.Fatalf("run %d exit %d", i, code)
		}
	}
	for _, c := range []struct {
		f    *apptest.Fake
		acct string
	}{{claude, "claude-main"}, {claude, "claude-rec"}, {codex, "codex-main"}} {
		if c.f.Calls[c.acct] != 1 {
			t.Errorf("%s adapter calls = %d, want 1", c.acct, c.f.Calls[c.acct])
		}
	}
}

func TestLsStates(t *testing.T) {
	claude := apptest.NewFake("claude")
	claude.Errs["claude-main"] = adapter.ErrTokenExpired
	claude.Errs["claude-rec"] = &adapter.HTTPError{Status: 429, RetryAfter: 10 * time.Minute}
	codex := apptest.NewFake("codex")
	codex.Readings["codex-main"] = apptest.ClaudeReading(1, 1)
	d := apptest.NewDeps(t, testConfig, claude, codex)
	stdout, _, code := run(d, "ls", "--json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var got []gotAccount
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"claude-main": "expired token",
		"claude-rec":  "rate-limited until 2026-10-22T12:10:00Z",
		"codex-main":  "ok",
	}
	for _, a := range got {
		if a.State != want[a.Account] {
			t.Errorf("%s state = %q, want %q", a.Account, a.State, want[a.Account])
		}
	}
	// A second run inside the retry window is rate-limited via the store.
	stdout, _, _ = run(d, "ls")
	if !strings.Contains(stdout, "rate-limited until 2026-10-22T12:10:00Z") {
		t.Errorf("second run lost the rate-limit state:\n%s", stdout)
	}
}

// TestLsStatesWithCachedReading: an expired token or a rate limit wins over
// ok even when the store answers from a cached reading.
func TestLsStatesWithCachedReading(t *testing.T) {
	claude := apptest.NewFake("claude")
	claude.Readings["claude-main"] = apptest.ClaudeReading(40, 10)
	claude.Readings["claude-rec"] = apptest.ClaudeReading(20, 10)
	codex := apptest.NewFake("codex")
	codex.Readings["codex-main"] = apptest.ClaudeReading(1, 1)
	d := apptest.NewDeps(t, testConfig, claude, codex)
	if _, _, code := run(d, "ls"); code != 0 {
		t.Fatalf("first run exit %d", code)
	}
	claude.Expired["claude-main"] = true
	claude.Errs["claude-rec"] = &adapter.HTTPError{Status: 429, RetryAfter: 10 * time.Minute}
	stdout, _, code := run(d, "ls", "--json", "--refresh")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var got []gotAccount
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"claude-main": "expired token",
		"claude-rec":  "rate-limited until 2026-10-22T12:10:00Z",
		"codex-main":  "ok",
	}
	for _, a := range got {
		if a.State != want[a.Account] {
			t.Errorf("%s state = %q, want %q", a.Account, a.State, want[a.Account])
		}
		if a.Account != "codex-main" && len(a.Windows) != 2 {
			t.Errorf("%s: want the cached reading's 2 windows, got %+v", a.Account, a.Windows)
		}
	}
}

func TestLsBadArgsAndConfig(t *testing.T) {
	d := apptest.NewDeps(t, `{"version": 2}`, apptest.NewFake("claude"))
	if _, stderr, code := run(d, "ls"); code != 1 || !strings.Contains(stderr, "version") {
		t.Errorf("config error: code %d stderr %q", code, stderr)
	}
	if _, _, code := run(d, "ls", "--bogus"); code != 1 {
		t.Errorf("unknown flag exit %d, want 1", code)
	}
}

func TestLsModels(t *testing.T) {
	claude := apptest.NewFake("claude")
	claude.ModelList["claude-main"] = []adapter.Model{
		{ID: "claude-opus-5-5", Thinking: []string{"low", "high"}},
		{ID: "claude-haiku-5"},
	}
	codex := apptest.NewFake("codex") // codex-main has no models entry → error
	d := apptest.NewDeps(t, testConfig, claude, codex)

	stdout, _, code := run(d, "ls", "models")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"ACCOUNT", "MODEL", "THINKING", "claude-opus-5-5", "low,high", "claude-haiku-5", "error: fake: no models"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stdout, "codex-main") {
		t.Errorf("failing account must still be listed:\n%s", stdout)
	}

	stdout, _, code = run(d, "ls", "models", "--json")
	if code != 0 {
		t.Fatalf("json exit %d", code)
	}
	var got []struct {
		Account string `json:"account"`
		Models  []struct {
			ID       string   `json:"id"`
			Thinking []string `json:"thinking"`
		} `json:"models"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	if len(got) != 3 || got[0].Account != "claude-main" || len(got[0].Models) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Models[1].Thinking == nil || len(got[0].Models[1].Thinking) != 0 {
		t.Errorf("thinking must be [] not null:\n%s", stdout)
	}
	if got[1].Error == "" || len(got[1].Models) != 0 {
		t.Errorf("claude-rec = %+v, want error and no models", got[1])
	}
}
