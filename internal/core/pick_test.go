package core

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
)

// fakeEnv serves canned credentials and usage states and counts Usage calls.
// An account missing from creds has a present credential; an account missing
// from usage has unknown usage.
type fakeEnv struct {
	creds  map[string]adapter.CredStatus
	usage  map[string]AccountState
	called map[string]int
}

func (e *fakeEnv) Credential(account string) adapter.CredStatus {
	if c, ok := e.creds[account]; ok {
		return c
	}
	return adapter.CredStatus{Present: true, Source: "test: " + account}
}

func (e *fakeEnv) Usage(account string) AccountState {
	if e.called == nil {
		e.called = map[string]int{}
	}
	e.called[account]++
	return e.usage[account]
}

func (e *fakeEnv) totalCalls() int {
	n := 0
	for _, c := range e.called {
		n += c
	}
	return n
}

const (
	hour = time.Hour
	day  = 24 * time.Hour
)

// later is a reset time that has not passed for any test's now.
var later = time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)

func win(label string, length time.Duration, used float64) adapter.Window {
	return adapter.Window{Label: label, Length: length, UsedPct: used, ResetsAt: later}
}

func reading(taken time.Time, ws ...adapter.Window) *adapter.Reading {
	return &adapter.Reading{TakenAt: taken, Windows: ws}
}

// claudeReading reports 5h, 7d and a Fable-scoped weekly window, so neither
// example Claude choice triggers a cap-length warning.
func claudeReading(taken time.Time, session, weekly float64) *adapter.Reading {
	fable := win("fable", 7*day, 10)
	fable.ScopeModelName = "Fable"
	return reading(taken, win("session", 5*hour, session), win("weekly", 7*day, weekly), fable)
}

func exampleConfig(t *testing.T) *Config {
	t.Helper()
	cfg, err := Load("../../examples/config.json", "/home/u")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// inlineConfig is the example's accounts and schedules with the given profiles.
func inlineConfig(t *testing.T, profiles string) *Config {
	t.Helper()
	data := `{"version": 1,
  "accounts": {
    "claude-main": {"vendor": "claude", "config_dir": "~/.claude"},
    "claude-rec": {"vendor": "claude", "config_dir": "~/.claude-rec"},
    "go-1": {"vendor": "opencode-go", "key_file": "~/.ssh/opencode-go-api", "key_line": 1}
  },
  "schedules": {"deepseek-peak": {"days": ["mon", "tue", "wed", "thu", "fri"], "utc": ["01:00-04:00", "06:00-10:00"]}},
  "profiles": ` + profiles + `}`
	cfg, err := Parse([]byte(data), "inline.json", "/home/u")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// exampleWithBadScheduleRef is examples/config.json with only
// profiles.tester[1].skip_during changed to ["deepseek-peek"] (ruling 2).
func exampleWithBadScheduleRef(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../examples/config.json")
	if err != nil {
		t.Fatal(err)
	}
	const ref = `"skip_during": ["deepseek-peak"]`
	text := string(data)
	if n := strings.Count(text, ref); n != 2 {
		t.Fatalf("example has %d %s, want 2 (tester[0] and tester[1])", n, ref)
	}
	second := strings.Index(text, ref) + len(ref)
	second += strings.Index(text[second:], ref)
	return []byte(text[:second] + `"skip_during": ["deepseek-peek"]` + text[second+len(ref):])
}

var (
	// noon is Thursday 12:00 UTC, outside deepseek-peak.
	noon = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	// peak is Thursday 07:30 UTC, inside deepseek-peak (case D).
	peak = time.Date(2026, 10, 22, 7, 30, 0, 0, time.UTC)
	// lateEvening is Thursday 22:30:11 UTC, off-peak, 88 min after go-2's reading (case E).
	lateEvening = time.Date(2026, 10, 22, 22, 30, 11, 0, time.UTC)
)

// goldenCase produces the result whose JSON is testdata/pick_<name>.json.
type goldenCase struct {
	name string
	run  func(t *testing.T) Result
	exit int
}

// goldenNames are the goldens in spec §7.1 order: blocks A–H, then I.
var goldenNames = []string{"a", "b", "c", "d", "e", "f", "g", "h", "lb"}

func TestPickGolden(t *testing.T) {
	cases := append(exampleGoldenCases(), groupGoldenCase())
	for i, tc := range cases {
		if tc.name != goldenNames[i] {
			t.Fatalf("case %d is %q, want %q (goldenNames order)", i, tc.name, goldenNames[i])
		}
		t.Run(tc.name, func(t *testing.T) {
			want, err := os.ReadFile("testdata/pick_" + tc.name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			r := tc.run(t)
			if got := r.JSON(); !bytes.Equal(got, want) {
				t.Errorf("JSON mismatch\n--- got\n%s--- want\n%s", got, want)
			}
			if got := r.ExitCode(); got != tc.exit {
				t.Errorf("ExitCode = %d, want %d", got, tc.exit)
			}
		})
	}
}

// groupGoldenCase is §7.1 block I: the example's tester with go-1 and go-2
// in one group. The shuffle tries go-2 first, over its 30d cap, then go-1.
func groupGoldenCase() goldenCase {
	return goldenCase{"lb", func(t *testing.T) Result {
		cfg := groupPickConfig(t, `{"tester": [
			{"load_balance": "random", "choices": [
				{"account": "go-1", "model": "opencode-go/deepseek-v4.1", "thinking": null,
					"cap": [95, 95, 98], "skip_during": ["deepseek-peak"]},
				{"account": "go-2", "model": "opencode-go/deepseek-v4.1", "thinking": null,
					"cap": [95, 95, 98], "skip_during": ["deepseek-peak"]}]},
			{"account": "claude-main", "model": "claude-sonnet-5-5", "thinking": "medium"}]}`)
		env := &fakeEnv{usage: map[string]AccountState{
			"go-1": {Reading: goReading(20, 60)},
			"go-2": {Reading: goReading(20, 98)},
		}}
		return Pick(cfg, "tester", noon, env, reverse)
	}, 0}
}

// exampleGoldenCases are §7.1 blocks A–H, against examples/config.json.
func exampleGoldenCases() []goldenCase {
	return []goldenCase{
		{"a", func(t *testing.T) Result {
			env := &fakeEnv{usage: map[string]AccountState{
				"claude-main": {Reading: claudeReading(noon, 40, 50)},
			}}
			return Pick(exampleConfig(t), "coder-difficult", noon, env, nil)
		}, 0},
		{"b", func(t *testing.T) Result {
			env := &fakeEnv{usage: map[string]AccountState{
				"claude-main": {Reading: claudeReading(noon, 92, 50)},
				"codex-main":  {Reading: reading(noon, win("primary_window", 5*hour, 30), win("secondary_window", 7*day, 60))},
			}}
			return Pick(exampleConfig(t), "coder-difficult", noon, env, nil)
		}, 0},
		{"c", func(t *testing.T) Result {
			env := &fakeEnv{usage: map[string]AccountState{
				"claude-main": {Reading: claudeReading(noon, 50, 98)},
				"codex-main":  {Err: errors.New("usage endpoint returned HTTP 401; log in to Codex with this account")},
			}}
			return Pick(exampleConfig(t), "coder-difficult", noon, env, nil)
		}, 2},
		{"d", func(t *testing.T) Result {
			return Pick(exampleConfig(t), "tester", peak, &fakeEnv{}, nil)
		}, 0},
		{"e", func(t *testing.T) Result {
			goWindows := func(monthly float64) *adapter.Reading {
				return reading(time.Date(2026, 10, 22, 21, 2, 11, 0, time.UTC),
					win("rolling", 5*hour, 20), win("weekly", 7*day, 40), win("monthly", 30*day, monthly))
			}
			env := &fakeEnv{usage: map[string]AccountState{
				"go-1": {Reading: goWindows(98)},
				"go-2": {Reading: goWindows(60), Warnings: []string{
					"go-2: using reading from 2026-10-22T21:02:11Z (88 min old); last call at 22:20:40Z failed: timeout",
				}},
			}}
			return Pick(exampleConfig(t), "tester", lateEvening, env, nil)
		}, 0},
		{"f", func(t *testing.T) Result {
			cfg := inlineConfig(t, `{"reviewer": [
				{"account": "claude-main", "model": "claude-opus-5-5", "thinking": "high", "cap": [90, 98]}]}`)
			env := &fakeEnv{usage: map[string]AccountState{
				"claude-main": {Reading: claudeReading(noon, 92, 50)},
			}}
			return Pick(cfg, "reviewer", noon, env, nil)
		}, 1},
		{"g", func(t *testing.T) Result {
			return Pick(exampleConfig(t), "coder-hard", noon, &fakeEnv{}, nil)
		}, 1},
		{"h", func(t *testing.T) Result {
			_, err := Parse(exampleWithBadScheduleRef(t), "~/.config/modelpick/config.json", "/home/u")
			if err == nil {
				t.Fatal("Parse accepted an unknown schedule reference")
			}
			return Failed("tester", err.Error())
		}, 1},
	}
}

// TestGoldenFilesMatchSpec keeps testdata/pick_*.json byte-equal to the
// spec's §7.1 blocks A–I, in goldenNames order.
func TestGoldenFilesMatchSpec(t *testing.T) {
	spec, err := os.ReadFile("../../docs/superpowers/specs/2026-10-22-modelpick-design.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(spec)
	start := strings.Index(text, "\n### 7.1 ")
	end := strings.Index(text, "\n## 8. ")
	if start < 0 || end < start {
		t.Fatal("cannot locate §7.1 in the spec")
	}
	parts := strings.Split(text[start:end], "```json\n")[1:]
	if len(parts) != len(goldenNames) {
		t.Fatalf("§7.1 has %d json blocks, want %d", len(parts), len(goldenNames))
	}
	for i, p := range parts {
		block, _, ok := strings.Cut(p, "```\n")
		if !ok {
			t.Fatalf("block %d is not closed", i)
		}
		name := "testdata/pick_" + goldenNames[i] + ".json"
		got, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != block {
			t.Errorf("%s differs from spec §7.1 block %c", name, 'A'+i)
		}
	}
}

func TestPickIsLazy(t *testing.T) {
	env := &fakeEnv{usage: map[string]AccountState{
		"claude-main": {Reading: claudeReading(noon, 40, 50)},
	}}
	r := Pick(exampleConfig(t), "coder-difficult", noon, env, nil)
	if r.Model == nil || *r.Model != "claude-opus-5-5" {
		t.Fatalf("picked %v, want claude-opus-5-5", r.Model)
	}
	if len(env.called) != 1 || env.called["claude-main"] == 0 {
		t.Errorf("Usage called for %v, want claude-main only", env.called)
	}

	cfg := inlineConfig(t, `{"p": [
		{"account": "claude-rec", "model": "claude-sonnet-5-5", "thinking": "medium"},
		{"account": "claude-main", "model": "claude-opus-5-5", "thinking": "high", "cap": [90, 98]}]}`)
	env = &fakeEnv{}
	r = Pick(cfg, "p", noon, env, nil)
	if r.Account == nil || *r.Account != "claude-rec" {
		t.Fatalf("picked %v, want claude-rec", r.Account)
	}
	if n := env.totalCalls(); n != 0 {
		t.Errorf("Usage called %d times for an uncapped first choice, want 0", n)
	}
}

func TestSkipBeatsUncapped(t *testing.T) {
	cfg := inlineConfig(t, `{"p": [
		{"account": "go-1", "model": "opencode-go/deepseek-v4.1", "thinking": null, "skip_during": ["deepseek-peak"]},
		{"account": "claude-rec", "model": "claude-sonnet-5-5", "thinking": "medium"}]}`)
	r := Pick(cfg, "p", peak, &fakeEnv{}, nil)
	if r.Account == nil || *r.Account != "claude-rec" {
		t.Fatalf("picked %v, want claude-rec", r.Account)
	}
	want := []Skip{{Account: "go-1", Model: "opencode-go/deepseek-v4.1", Why: "in schedule deepseek-peak"}}
	if fmt.Sprint(r.Skipped) != fmt.Sprint(want) {
		t.Errorf("Skipped = %v, want %v", r.Skipped, want)
	}
}

func TestCapBoundaries(t *testing.T) {
	cases := []struct {
		used, cap float64
		why       string // "" = picked
	}{
		{100, 100, "5h window 100% >= cap 100"},
		{104, 98, "5h window 104% >= cap 98"},
		{0, 0, "5h window 0% >= cap 0"},
		{97.5, 98, ""},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("used %v cap %v", tc.used, tc.cap), func(t *testing.T) {
			cfg := inlineConfig(t, fmt.Sprintf(`{"p": [
				{"account": "claude-main", "model": "claude-opus-5-5", "thinking": "high", "cap": [%v]}]}`, tc.cap))
			env := &fakeEnv{usage: map[string]AccountState{
				"claude-main": {Reading: reading(noon, win("session", 5*hour, tc.used))},
			}}
			r := Pick(cfg, "p", noon, env, nil)
			if tc.why == "" {
				if r.Status != StatusOK || len(r.Skipped) != 0 {
					t.Fatalf("want picked, got status %s skipped %v", r.Status, r.Skipped)
				}
				return
			}
			if r.Status != StatusFailed || len(r.Skipped) != 1 || r.Skipped[0].Why != tc.why {
				t.Fatalf("want blocked with %q, got status %s skipped %v", tc.why, r.Status, r.Skipped)
			}
		})
	}
}

func TestResetWindowReadsZero(t *testing.T) {
	cfg := inlineConfig(t, `{"p": [
		{"account": "claude-main", "model": "claude-opus-5-5", "thinking": "high", "cap": [90]}]}`)
	w := win("session", 5*hour, 100)
	w.ResetsAt = noon.Add(-time.Minute)
	env := &fakeEnv{usage: map[string]AccountState{"claude-main": {Reading: reading(noon.Add(-hour), w)}}}
	if r := Pick(cfg, "p", noon, env, nil); r.Status != StatusOK {
		t.Fatalf("a window whose reset has passed must read 0%%, got status %s skipped %v", r.Status, r.Skipped)
	}
}

func TestScheduleSkipIsNotAnError(t *testing.T) {
	r := Pick(exampleConfig(t), "tester", peak, &fakeEnv{}, nil)
	if r.Status != StatusOK || len(r.Errors) != 0 || len(r.Skipped) != 2 {
		t.Fatalf("status %s errors %v skipped %v", r.Status, r.Errors, r.Skipped)
	}
}

func TestExpiredCredentialDoesNotSkipUncapped(t *testing.T) {
	cfg := inlineConfig(t, `{"p": [
		{"account": "claude-rec", "model": "claude-sonnet-5-5", "thinking": "medium"}]}`)
	env := &fakeEnv{creds: map[string]adapter.CredStatus{
		"claude-rec": {Present: true, Expired: true, Source: "keychain: x"},
	}}
	r := Pick(cfg, "p", noon, env, nil)
	if r.Status != StatusOK || r.Account == nil || *r.Account != "claude-rec" {
		t.Fatalf("status %s account %v skipped %v", r.Status, r.Account, r.Skipped)
	}
}

func TestMissingCredentialIsAnError(t *testing.T) {
	cfg := inlineConfig(t, `{"p": [
		{"account": "claude-main", "model": "claude-opus-5-5", "thinking": "high"},
		{"account": "go-1", "model": "opencode-go/deepseek-v4.1", "thinking": null},
		{"account": "claude-rec", "model": "claude-sonnet-5-5", "thinking": "medium"}]}`)
	env := &fakeEnv{creds: map[string]adapter.CredStatus{
		"claude-main": {Source: "keychain: Claude Code-credentials"},
		"go-1":        {Present: true, Source: "file", Err: errors.New("key line 1 absent in /home/u/.ssh/opencode-go-api")},
	}}
	r := Pick(cfg, "p", noon, env, nil)
	if r.Status != StatusOKWithErrors || r.ExitCode() != 2 || *r.Account != "claude-rec" {
		t.Fatalf("status %s account %v", r.Status, r.Account)
	}
	wantErrs := []RunError{
		{Account: "claude-main", Error: "keychain: Claude Code-credentials: not found"},
		{Account: "go-1", Error: "key line 1 absent in /home/u/.ssh/opencode-go-api"},
	}
	if fmt.Sprint(r.Errors) != fmt.Sprint(wantErrs) {
		t.Errorf("Errors = %v, want %v", r.Errors, wantErrs)
	}
	for _, s := range r.Skipped {
		if s.Why != "credential missing (see errors)" {
			t.Errorf("skip %v: wrong why", s)
		}
	}
	if len(r.Skipped) != 2 {
		t.Errorf("Skipped = %v, want 2", r.Skipped)
	}
}

func TestAccountReadOnceAcrossChoices(t *testing.T) {
	// Both claude-main choices are blocked by the same failed read: the error
	// and the state's warning appear once, and Usage is called once.
	env := &fakeEnv{usage: map[string]AccountState{
		"claude-main": {Err: errors.New("timeout"), Warnings: []string{"claude-main: note"}},
		"codex-main":  {Err: errors.New("timeout")},
	}}
	r := Pick(exampleConfig(t), "coder-difficult", noon, env, nil)
	if r.Status != StatusOKWithErrors || *r.Account != "claude-rec" {
		t.Fatalf("status %s account %v", r.Status, r.Account)
	}
	if len(r.Errors) != 2 || len(r.Warnings) != 1 || len(r.Skipped) != 3 {
		t.Errorf("errors %v warnings %v skipped %v", r.Errors, r.Warnings, r.Skipped)
	}
	if env.called["claude-main"] != 1 {
		t.Errorf("Usage(claude-main) called %d times, want 1", env.called["claude-main"])
	}
}

func TestFailedHasEmptyListsNotNull(t *testing.T) {
	got := string(Failed("p", "x").JSON())
	for _, k := range []string{`"skipped": []`, `"errors": []`, `"warnings": []`, `"account_ref": null`} {
		if !strings.Contains(got, k) {
			t.Errorf("missing %s in\n%s", k, got)
		}
	}
}

func TestUnknownUsageWithoutErrIsAnError(t *testing.T) {
	cfg := inlineConfig(t, `{"p": [
		{"account": "claude-main", "model": "claude-opus-5-5", "thinking": "high", "cap": [90, 98]},
		{"account": "claude-rec", "model": "claude-sonnet-5-5", "thinking": "medium"}]}`)
	r := Pick(cfg, "p", noon, &fakeEnv{}, nil) // claude-main: Reading nil, Err nil
	if r.Status != StatusOKWithErrors || *r.Account != "claude-rec" {
		t.Fatalf("status %s account %v", r.Status, r.Account)
	}
	wantSkip := []Skip{{Account: "claude-main", Model: "claude-opus-5-5", Why: "usage unknown (see errors)"}}
	wantErrs := []RunError{{Account: "claude-main", Error: "usage unavailable"}}
	if fmt.Sprint(r.Skipped) != fmt.Sprint(wantSkip) || fmt.Sprint(r.Errors) != fmt.Sprint(wantErrs) {
		t.Errorf("skipped %v errors %v, want %v %v", r.Skipped, r.Errors, wantSkip, wantErrs)
	}
}

func TestFailedCallStillJudgesOlderReading(t *testing.T) {
	cfg := inlineConfig(t, `{"p": [
		{"account": "claude-main", "model": "claude-opus-5-5", "thinking": "high", "cap": [90, 98]},
		{"account": "claude-rec", "model": "claude-sonnet-5-5", "thinking": "medium"}]}`)
	older := noon.Add(-hour)
	cases := []struct {
		session     float64
		wantAccount string
		wantSkipped []Skip
	}{
		{40, "claude-main", nil},
		{92, "claude-rec", []Skip{{Account: "claude-main", Model: "claude-opus-5-5", Why: "5h window 92% >= cap 90"}}},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.session), func(t *testing.T) {
			env := &fakeEnv{usage: map[string]AccountState{"claude-main": {
				Reading: reading(older, win("session", 5*hour, tc.session), win("weekly", 7*day, 50)),
				Err:     errors.New("usage endpoint returned HTTP 503"),
			}}}
			r := Pick(cfg, "p", noon, env, nil)
			if r.Status != StatusOKWithErrors || *r.Account != tc.wantAccount {
				t.Fatalf("status %s account %v, want ok_with_errors %s", r.Status, *r.Account, tc.wantAccount)
			}
			if fmt.Sprint(r.Skipped) != fmt.Sprint(tc.wantSkipped) {
				t.Errorf("skipped %v, want %v", r.Skipped, tc.wantSkipped)
			}
			wantErrs := []RunError{{Account: "claude-main", Error: "usage endpoint returned HTTP 503"}}
			if fmt.Sprint(r.Errors) != fmt.Sprint(wantErrs) {
				t.Errorf("errors %v, want %v", r.Errors, wantErrs)
			}
		})
	}
}

func TestNoApplicableWindowsIsUnknownUsage(t *testing.T) {
	fableOnly := win("fable", 7*day, 10)
	fableOnly.ScopeModelName = "Fable"
	cases := map[string]*adapter.Reading{
		"empty":         reading(noon),
		"other's scope": reading(noon, fableOnly),
	}
	for name, rd := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := inlineConfig(t, `{"p": [
				{"account": "claude-main", "model": "claude-opus-5-5", "thinking": "high", "cap": [90, 98]},
				{"account": "claude-rec", "model": "claude-sonnet-5-5", "thinking": "medium"}]}`)
			env := &fakeEnv{usage: map[string]AccountState{"claude-main": {Reading: rd}}}
			r := Pick(cfg, "p", noon, env, nil)
			if r.Status != StatusOKWithErrors || *r.Account != "claude-rec" {
				t.Fatalf("status %s account %v", r.Status, r.Account)
			}
			wantSkip := []Skip{{Account: "claude-main", Model: "claude-opus-5-5", Why: "usage unknown (no windows reported)"}}
			wantErrs := []RunError{{Account: "claude-main", Error: "usage response had no windows"}}
			if fmt.Sprint(r.Skipped) != fmt.Sprint(wantSkip) || fmt.Sprint(r.Errors) != fmt.Sprint(wantErrs) {
				t.Errorf("skipped %v errors %v, want %v %v", r.Skipped, r.Errors, wantSkip, wantErrs)
			}
			if len(r.Warnings) != 0 {
				t.Errorf("warnings %v, want none", r.Warnings)
			}
		})
	}
}

func TestPickedThinkingIsACopy(t *testing.T) {
	cfg := inlineConfig(t, `{"p": [
		{"account": "claude-rec", "model": "claude-sonnet-5-5", "thinking": "medium"}]}`)
	r := Pick(cfg, "p", noon, &fakeEnv{}, nil)
	*r.Thinking = "changed"
	if got := *cfg.Profiles["p"][0].Thinking; got != "medium" {
		t.Errorf("config thinking became %q through the result", got)
	}
}

func TestAllowDuringSkipReasons(t *testing.T) {
	cfg := inlineConfig(t, `{"p": [
		{"account": "go-1", "model": "m1", "thinking": null, "allow_during": ["deepseek-peak"]},
		{"account": "go-1", "model": "m2", "thinking": null, "allow_during": ["deepseek-peak", "deepseek-peak"]},
		{"account": "claude-rec", "model": "claude-sonnet-5-5", "thinking": "medium"}]}`)
	r := Pick(cfg, "p", noon, &fakeEnv{}, nil) // Thursday 12:00 is outside every peak range
	if len(r.Skipped) != 2 || r.Skipped[0].Why != "outside schedule deepseek-peak" ||
		r.Skipped[1].Why != "outside schedules deepseek-peak, deepseek-peak" {
		t.Fatalf("Skipped = %v", r.Skipped)
	}
	r = Pick(cfg, "p", peak, &fakeEnv{}, nil)
	if r.Model == nil || *r.Model != "m1" {
		t.Fatalf("inside the schedule must pick m1, got %v", r.Model)
	}
}
