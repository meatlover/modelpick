package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/app/apptest"
)

// lsFakes: claude-main ok, claude-secondary unauthorized, codex-main ok.
func lsFakes() (*apptest.Fake, *apptest.Fake) {
	claude := apptest.NewFake("claude")
	claude.Readings["claude-main"] = apptest.ClaudeReading(40, 10)
	claude.Errs["claude-secondary"] = adapter.ErrUnauthorized
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
	if got := strings.Fields(lines[0]); strings.Join(got, " ") != "ACCOUNT STATE WINDOW USED RESETS AGE" {
		t.Fatalf("header = %q", lines[0])
	}
	for _, name := range []string{"claude-main", "claude-secondary", "codex-main"} {
		if !strings.Contains(stdout, name) {
			t.Errorf("account %s missing:\n%s", name, stdout)
		}
	}
	var recRow string
	for _, l := range lines {
		if strings.HasPrefix(l, "claude-secondary") {
			recRow = l
			break
		}
	}
	if strings.Fields(recRow)[1] != "error" {
		t.Errorf("claude-secondary row = %q, want STATE error", recRow)
	}
	if last := lines[len(lines)-1]; !strings.HasPrefix(last, "claude-secondary: usage endpoint returned HTTP 401; log in to Claude Code") {
		t.Errorf("last line = %q, want the claude-secondary error line", last)
	}
	// the account name opens its first line only.
	if n := strings.Count(stdout, "claude-main"); n != 1 {
		t.Errorf("claude-main lines = %d, want 1:\n%s", n, stdout)
	}
	if !strings.Contains(stdout, "40%") || !strings.Contains(stdout, "5h") {
		t.Errorf("window columns missing:\n%s", stdout)
	}
}

const lsGolden = `ACCOUNT           STATE  WINDOW    USED  RESETS                    AGE
claude-main       ok     5h         31%  in 1h 50m     Thu 13:50Z  4m
                         7d         58%  in 2d 12h     Sun 00:00Z
                         7d Fable   12%  in 2d 12h     Sun 00:00Z
claude-secondary  error  -            -  -                         -
codex-main        ok     7d         41%  in 8d      Oct 30 12:00Z  12m

claude-secondary: usage endpoint returned HTTP 401; log in to Claude Code with this account
`

func TestLsTableGolden(t *testing.T) {
	now, wk := apptest.Now, 7*24*time.Hour
	claude := apptest.NewFake("claude")
	claude.Readings["claude-main"] = adapter.Reading{TakenAt: now.Add(-4 * time.Minute), Windows: []adapter.Window{
		{Label: "session", Length: 5 * time.Hour, UsedPct: 31, ResetsAt: now.Add(110 * time.Minute)},
		{Label: "weekly", Length: wk, UsedPct: 58, ResetsAt: now.Add(60 * time.Hour)},
		{Label: "weekly", Length: wk, UsedPct: 12, ResetsAt: now.Add(60 * time.Hour), ScopeModelName: "Fable"},
	}}
	claude.Errs["claude-secondary"] = adapter.ErrUnauthorized
	codex := apptest.NewFake("codex")
	codex.Readings["codex-main"] = adapter.Reading{TakenAt: now.Add(-12 * time.Minute), Windows: []adapter.Window{
		{Label: "primary_window", Length: wk, UsedPct: 41, ResetsAt: now.Add(8 * 24 * time.Hour)},
	}}
	d := apptest.NewDeps(t, testConfig, claude, codex)
	stdout, stderr, code := run(d, "ls")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if stdout != lsGolden {
		t.Errorf("got:\n%s\nwant:\n%s", stdout, lsGolden)
	}
}

// lsUnknownGolden: a window of unknown length shows the vendor's label and
// one with no reset time shows "-" (Ruling 2). That window comes first, so
// only the continuation line has a reset; RESETS must still be wide enough
// for it, keeping AGE in its column.
const lsUnknownGolden = `ACCOUNT      STATE  WINDOW   USED  RESETS                 AGE
claude-main  ok     monthly   20%  -                      4m
                    7d        58%  in 2d 12h  Sun 00:00Z
`

func TestLsTableUnknownLengthAndReset(t *testing.T) {
	now := apptest.Now
	claude := apptest.NewFake("claude")
	claude.Readings["claude-main"] = adapter.Reading{TakenAt: now.Add(-4 * time.Minute), Windows: []adapter.Window{
		{Label: "monthly", UsedPct: 20},
		{Label: "weekly", Length: 7 * 24 * time.Hour, UsedPct: 58, ResetsAt: now.Add(60 * time.Hour)},
	}}
	stdout, stderr, code := run(apptest.NewDeps(t, testConfig, claude, apptest.NewFake("codex")), "ls", "claude-main")
	if code != 0 || stderr != "" || stdout != lsUnknownGolden {
		t.Errorf("exit %d stderr %q; got:\n%s\nwant:\n%s", code, stderr, stdout, lsUnknownGolden)
	}
}

func TestLsTablePassedReset(t *testing.T) {
	claude := apptest.NewFake("claude")
	claude.Readings["claude-main"] = adapter.Reading{TakenAt: apptest.Now, Windows: []adapter.Window{
		{Label: "session", Length: 5 * time.Hour, UsedPct: 70, ResetsAt: apptest.Now.Add(-10 * time.Minute)},
	}}
	d := apptest.NewDeps(t, testConfig, claude, apptest.NewFake("codex"))
	stdout, _, code := run(d, "ls", "claude-main")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	row := strings.Fields(strings.Split(stdout, "\n")[1])
	if got := strings.Join(row[2:], " "); got != "5h 0% in 0m Thu 11:50Z <1m" {
		t.Errorf("row = %q\n%s", got, stdout)
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
	if len(got) != 3 || got[0].Account != "claude-main" || got[1].Account != "claude-secondary" || got[2].Account != "codex-main" {
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
		t.Errorf("claude-secondary = %+v", rec)
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
	}{{claude, "claude-main"}, {claude, "claude-secondary"}, {codex, "codex-main"}} {
		if c.f.Calls[c.acct] != 1 {
			t.Errorf("%s adapter calls = %d, want 1", c.acct, c.f.Calls[c.acct])
		}
	}
}

func TestLsStates(t *testing.T) {
	claude := apptest.NewFake("claude")
	claude.Errs["claude-main"] = adapter.ErrTokenExpired
	claude.Errs["claude-secondary"] = &adapter.HTTPError{Status: 429, RetryAfter: 10 * time.Minute}
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
		"claude-main":      "expired token",
		"claude-secondary": "rate-limited until 2026-10-22T12:10:00Z",
		"codex-main":       "ok",
	}
	for _, a := range got {
		if a.State != want[a.Account] {
			t.Errorf("%s state = %q, want %q", a.Account, a.State, want[a.Account])
		}
	}
	// A second run inside the retry window is rate-limited via the store.
	stdout, _, _ = run(d, "ls")
	// The rate limit stays in the STATE column (second); the stored failure of
	// claude-main shows "error" there and its message in a line below.
	for _, l := range strings.Split(stdout, "\n") {
		f := strings.Fields(l)
		if strings.HasPrefix(l, "claude-secondary ") && strings.Join(f[1:3], " ") != "rate-limited until" {
			t.Errorf("claude-secondary STATE column wrong: %q", l)
		}
		if strings.HasPrefix(l, "claude-main ") && f[1] != "error" {
			t.Errorf("claude-main STATE column wrong: %q", l)
		}
	}
	if !strings.Contains(stdout, "\n\nclaude-main: no usage reading; last call at") {
		t.Errorf("claude-main error line missing below the table:\n%s", stdout)
	}
	if strings.Contains(stdout, "claude-secondary:") {
		t.Errorf("a rate limit produced an error line:\n%s", stdout)
	}
}

// TestLsStatesWithCachedReading: an expired token or a rate limit wins over
// ok even when the store answers from a cached reading.
func TestLsStatesWithCachedReading(t *testing.T) {
	claude := apptest.NewFake("claude")
	claude.Readings["claude-main"] = apptest.ClaudeReading(40, 10)
	claude.Readings["claude-secondary"] = apptest.ClaudeReading(20, 10)
	codex := apptest.NewFake("codex")
	codex.Readings["codex-main"] = apptest.ClaudeReading(1, 1)
	d := apptest.NewDeps(t, testConfig, claude, codex)
	if _, _, code := run(d, "ls"); code != 0 {
		t.Fatalf("first run exit %d", code)
	}
	claude.Expired["claude-main"] = true
	claude.Errs["claude-secondary"] = &adapter.HTTPError{Status: 429, RetryAfter: 10 * time.Minute}
	stdout, _, code := run(d, "ls", "--json", "--refresh")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var got []gotAccount
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"claude-main":      "expired token",
		"claude-secondary": "rate-limited until 2026-10-22T12:10:00Z",
		"codex-main":       "ok",
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
		t.Errorf("claude-secondary = %+v, want error and no models", got[1])
	}
}

func TestLsNamedAccountsInOrder(t *testing.T) {
	claude, codex := lsFakes()
	d := apptest.NewDeps(t, testConfig, claude, codex)
	stdout, stderr, code := run(d, "ls", "codex-main", "--json", "claude-main", "codex-main")
	if code != 0 {
		t.Fatalf("exit %d; stderr %q", code, stderr)
	}
	var got []gotAccount
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	if len(got) != 2 || got[0].Account != "codex-main" || got[1].Account != "claude-main" {
		t.Fatalf("accounts = %+v, want [codex-main claude-main]", got)
	}
	if claude.Calls["claude-secondary"] != 0 {
		t.Errorf("claude-secondary was read %d times", claude.Calls["claude-secondary"])
	}
}

func TestLsUnknownAccount(t *testing.T) {
	claude, codex := lsFakes()
	d := apptest.NewDeps(t, testConfig, claude, codex)
	stdout, stderr, code := run(d, "ls", "claude-main", "go-9")
	want := "modelpick: ls: unknown account \"go-9\"; configured: claude-main, claude-secondary, codex-main\n"
	if code != 1 || stdout != "" || stderr != want {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if n := claude.TotalUsage() + codex.TotalUsage(); n != 0 {
		t.Errorf("usage calls = %d, want 0", n)
	}
}

func TestLsModelsOnlyFirst(t *testing.T) {
	claude, codex := lsFakes()
	d := apptest.NewDeps(t, testConfig, claude, codex)
	_, stderr, code := run(d, "ls", "claude-main", "models")
	if code != 1 || !strings.Contains(stderr, `unknown account "models"`) {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
}

func TestLsRefreshOnlyNamed(t *testing.T) {
	claude, codex := lsFakes()
	d := apptest.NewDeps(t, testConfig, claude, codex)
	if _, _, code := run(d, "ls"); code != 0 {
		t.Fatalf("seed exit %d", code)
	}
	main0, rec0 := claude.Calls["claude-main"], claude.Calls["claude-secondary"]
	if _, _, code := run(d, "ls", "claude-main", "--refresh"); code != 0 {
		t.Fatalf("refresh exit %d", code)
	}
	if claude.Calls["claude-main"] != main0+1 || claude.Calls["claude-secondary"] != rec0 {
		t.Errorf("calls main %d->%d, rec %d->%d", main0, claude.Calls["claude-main"], rec0, claude.Calls["claude-secondary"])
	}
}

func TestLsRenewsExpired(t *testing.T) {
	claude := apptest.NewFake("claude")
	claude.Renewable["claude-main"] = true
	claude.Expired["claude-main"] = true
	claude.Readings["claude-main"] = apptest.ClaudeReading(40, 10)
	d := apptest.NewDeps(t, testConfig, claude)
	stdout, stderr, code := run(d, "ls", "--json", "claude-main")
	if code != 0 {
		t.Fatalf("exit %d; stderr %q", code, stderr)
	}
	var rows []struct{ Account, State string }
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil || len(rows) != 1 {
		t.Fatalf("output %q: %v", stdout, err)
	}
	if rows[0].State != "ok" {
		t.Errorf("state = %q, want ok", rows[0].State)
	}
	if claude.RenewCalls["claude-main"] != 1 || claude.Calls["claude-main"] != 1 {
		t.Errorf("renew calls %d, usage calls %d, want 1 and 1", claude.RenewCalls["claude-main"], claude.Calls["claude-main"])
	}
}
