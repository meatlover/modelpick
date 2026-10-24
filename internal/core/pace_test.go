package core

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
)

func paceConfig(t *testing.T, cap string) *Config {
	t.Helper()
	return inlineConfig(t, `{"p": [
		{"account": "claude-main", "model": "claude-opus-5-5", "thinking": "high", "cap": `+cap+`},
		{"account": "claude-rec", "model": "claude-sonnet-5-5", "thinking": "medium"}]}`)
}

func paceWin(length time.Duration, used float64, resetsIn time.Duration) adapter.Window {
	return adapter.Window{Label: "w", Length: length, UsedPct: used, ResetsAt: noon.Add(resetsIn)}
}

func pickPace(t *testing.T, cap string, w adapter.Window) Result {
	t.Helper()
	env := &fakeEnv{usage: map[string]AccountState{"claude-main": {Reading: reading(noon, w)}}}
	return Pick(paceConfig(t, cap), "p", noon, env)
}

func wantSkip(t *testing.T, r Result, why string) {
	t.Helper()
	if len(r.Skipped) != 1 || r.Skipped[0].Why != why {
		t.Fatalf("skipped = %+v, want one skip %q", r.Skipped, why)
	}
}

func TestPaceReason(t *testing.T) {
	// resets in 61% of the window: elapsed is exactly 39%, so pace is 49%.
	r := pickPace(t, `["pace+10"]`, paceWin(7*day, 58, 7*day*61/100))
	wantSkip(t, r, "7d window 58% >= pace 49% (day 2.7 of 7, +10)")
}

func TestPaceFractionalFigure(t *testing.T) {
	// resets in 4.3 days: elapsed 38.571…%, pace 48.571…% prints as 48.6%.
	r := pickPace(t, `["pace+10"]`, paceWin(7*day, 58, 43*day/10))
	wantSkip(t, r, "7d window 58% >= pace 48.6% (day 2.7 of 7, +10)")
}

func TestPaceHourWindow(t *testing.T) {
	r := pickPace(t, `["pace+5"]`, paceWin(5*hour, 60, 5*hour/2))
	wantSkip(t, r, "5h window 60% >= pace 55% (hour 2.5 of 5, +5)")
}

func TestPaceNoReset(t *testing.T) {
	w := adapter.Window{Label: "w", Length: 7 * day, UsedPct: 96}
	r := pickPace(t, `["pace+10"]`, w)
	wantSkip(t, r, "7d window 96% >= cap 95")
	want := "claude-main: 7d window has no reset time; pace cap used default_cap 95"
	if len(r.Warnings) != 1 || r.Warnings[0] != want {
		t.Errorf("warnings = %q, want [%q]", r.Warnings, want)
	}
	r = pickPace(t, `["pace+10"]`, adapter.Window{Label: "w", Length: 7 * day, UsedPct: 90})
	if len(r.Skipped) != 0 || len(r.Warnings) != 1 {
		t.Errorf("used 90 under default_cap 95: skipped %v warnings %q", r.Skipped, r.Warnings)
	}
}

func TestPaceResetPassed(t *testing.T) {
	r := pickPace(t, `["pace+5"]`, paceWin(7*day, 80, -hour))
	if len(r.Skipped) != 0 || *r.Account != "claude-main" {
		t.Errorf("a reset window should be usable: skipped %v, account %v", r.Skipped, r.Account)
	}
}

func TestPaceMinimumAtReset(t *testing.T) {
	r := pickPace(t, `["pace+5"]`, paceWin(7*day, 0, 7*day))
	if len(r.Skipped) != 0 || *r.Account != "claude-main" {
		t.Errorf("pace+5 at elapsed 0, used 0 should be usable: skipped %v", r.Skipped)
	}
	// and the comparison is >=: used exactly at the allowance blocks.
	r = pickPace(t, `["pace+5"]`, paceWin(7*day, 5, 7*day))
	wantSkip(t, r, "7d window 5% >= pace 5% (day 0 of 7, +5)")
}

func TestPaceCapped100(t *testing.T) {
	w := paceWin(100*hour, 0, 5*hour) // elapsed 95
	allowed, elapsed, ok := PaceAllowed(w, 50, noon)
	if !ok || elapsed != 95 || allowed != 100 {
		t.Errorf("PaceAllowed = %v, %v, %v; want 100, 95, true", allowed, elapsed, ok)
	}
}

func TestPaceAllowedUnknown(t *testing.T) {
	if _, _, ok := PaceAllowed(adapter.Window{Length: day}, 10, noon); ok {
		t.Error("zero ResetsAt must be not ok")
	}
	if _, _, ok := PaceAllowed(adapter.Window{ResetsAt: later}, 10, noon); ok {
		t.Error("zero Length must be not ok")
	}
}

func TestPaceGolden(t *testing.T) {
	env := &fakeEnv{usage: map[string]AccountState{"claude-main": {Reading: reading(noon,
		paceWin(5*hour, 60, 5*hour/2), paceWin(7*day, 30, 43*day/10))}}}
	r := Pick(paceConfig(t, `["pace+5", "pace+10"]`), "p", noon, env)
	want, err := os.ReadFile("testdata/pick_pace.json")
	if err != nil {
		t.Fatal(err)
	}
	if got := r.JSON(); !bytes.Equal(got, want) {
		t.Errorf("JSON mismatch\n--- got\n%s--- want\n%s", got, want)
	}
}
