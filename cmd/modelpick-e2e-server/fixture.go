package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/app/apptest"
)

// fixtureConfig has no doctor finding with fixtureFakes and the readings of
// writeReadings: two used opencode-go accounts, an unused account
// (claude-spare), a used schedule (deepseek-peak) and an unused one (night),
// and profiles of three and four choices. Every cap has one value per
// window of its account's reading.
const fixtureConfig = `{
  "version": 1,
  "settings": { "min_call_interval_minutes": 15, "default_cap": 95 },
  "accounts": {
    "claude-main":  { "vendor": "claude", "config_dir": "~/.claude" },
    "claude-spare": { "vendor": "claude", "config_dir": "~/.claude-spare" },
    "codex-main":   { "vendor": "codex", "codex_home": "~/.codex" },
    "go-1": { "vendor": "opencode-go", "key_file": "~/.ssh/opencode-go-api", "key_line": 1 },
    "go-2": { "vendor": "opencode-go", "key_file": "~/.ssh/opencode-go-api", "key_line": 2 }
  },
  "schedules": {
    "deepseek-peak": { "days": ["mon", "tue", "wed", "thu", "fri"], "utc": ["01:00-04:00", "06:00-10:00"] },
    "night": { "days": ["sat", "sun"], "utc": ["22:00-23:00"] }
  },
  "profiles": {
    "coder": [
      { "account": "claude-main", "model": "claude-opus-5-5", "thinking": "high", "cap": [90, 98] },
      { "account": "codex-main", "model": "gpt-6.1-sol", "thinking": "high", "cap": [90, 98] },
      { "account": "go-1", "model": "opencode-go/deepseek-v4.1", "thinking": null, "cap": [95, 95, 98] },
      { "account": "claude-main", "model": "claude-sonnet-5-5", "thinking": "medium" }
    ],
    "tester": [
      { "account": "go-1", "model": "opencode-go/deepseek-v4.1", "thinking": null,
        "cap": [95, 95, 98], "skip_during": ["deepseek-peak"] },
      { "account": "go-2", "model": "opencode-go/deepseek-v4.1", "thinking": null,
        "cap": [95, 95, 98], "skip_during": ["deepseek-peak"] },
      { "account": "claude-main", "model": "claude-sonnet-5-5", "thinking": "medium" }
    ]
  }
}`

// vendorTool is the binary each vendor's Check looks for on PATH.
var vendorTool = map[string]string{"claude": "claude", "codex": "codex", "opencode-go": "opencode"}

// vendorModels is what every account of a vendor lists.
var vendorModels = map[string][]adapter.Model{
	"claude": {
		{ID: "claude-opus-5-5", Thinking: []string{"low", "medium", "high"}},
		{ID: "claude-sonnet-5-5", Thinking: []string{"low", "medium", "high"}},
		{ID: "claude-fable-5-1", Thinking: []string{"low", "medium", "high"}},
	},
	"codex":       {{ID: "gpt-6.1-sol", Thinking: []string{"low", "medium", "high"}}},
	"opencode-go": {{ID: "opencode-go/deepseek-v4.1"}, {ID: "opencode-go/kimi-k3"}},
}

// vendorWindows is the window lengths of each vendor's reading.
var vendorWindows = map[string][]time.Duration{
	"claude":      {5 * time.Hour, 7 * 24 * time.Hour},
	"codex":       {5 * time.Hour, 7 * 24 * time.Hour},
	"opencode-go": {5 * time.Hour, 7 * 24 * time.Hour, 30 * 24 * time.Hour},
}

// accountVendor is the vendor of each fixture account.
var accountVendor = map[string]string{
	"claude-main": "claude", "claude-spare": "claude", "codex-main": "codex",
	"go-1": "opencode-go", "go-2": "opencode-go",
}

// storedAge is how old each account's stored reading is at start. Only
// codex-main's is older than the 15-minute floor, so Check all calls exactly
// one account (age is not a doctor finding).
var storedAge = map[string]time.Duration{
	"claude-main": time.Minute, "claude-spare": time.Minute, "codex-main": 20 * time.Minute,
	"go-1": time.Minute, "go-2": time.Minute,
}

// reading is a vendor's reading taken at taken (zero: the store stamps the
// call's time), its windows resetting a day after now.
func reading(vendor string, taken, now time.Time) adapter.Reading {
	r := adapter.Reading{TakenAt: taken}
	for i, l := range vendorWindows[vendor] {
		r.Windows = append(r.Windows, adapter.Window{
			Label: []string{"session", "weekly", "monthly"}[i], Length: l,
			UsedPct: float64(10 * (i + 1)), ResetsAt: now.Add(24 * time.Hour),
		})
	}
	return r
}

// fixtureFakes are the three vendors' fakes: every account healthy, its
// models listed, and its usage call answering a fresh reading.
func fixtureFakes(now time.Time) map[string]*apptest.Fake {
	fakes := map[string]*apptest.Fake{}
	for v := range vendorTool {
		fakes[v] = apptest.NewFake(v)
	}
	for name, v := range accountVendor {
		fakes[v].ModelList[name] = vendorModels[v]
		fakes[v].Readings[name] = reading(v, time.Time{}, now)
	}
	return fakes
}

// storedState is usage.Store's state file: the fields a fresh good reading
// sets.
type storedState struct {
	LastGood    *adapter.Reading `json:"last_good"`
	LastAttempt time.Time        `json:"last_attempt"`
}

// writeReadings stores each account's reading, storedAge before now, where
// the store's Saved finds it.
func writeReadings(d app.Deps, now time.Time) error {
	cfg, err := app.LoadConfig(d)
	if err != nil {
		return err
	}
	dir := app.NewStore(d, cfg).Dir
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for name, v := range accountVendor {
		taken := now.Add(-storedAge[name])
		r := reading(v, taken, now)
		b, err := json.Marshal(storedState{LastGood: &r, LastAttempt: taken})
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name+".json"), b, 0o600); err != nil {
			return err
		}
	}
	return nil
}
