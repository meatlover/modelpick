// Package apptest holds the fakes shared by the test suites of app and its
// callers: a canned vendor adapter and a Deps that touches nothing real.
package apptest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/app"
)

// Now is the fixed clock of every test; readings reset well after Later.
var (
	Now   = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	Later = Now.Add(72 * time.Hour)
)

// Fake serves canned usage per account name and counts calls. An
// account missing from Readings and Errs has unknown usage (ErrShape).
type Fake struct {
	vendor    string
	Readings  map[string]adapter.Reading
	Errs      map[string]error
	Calls     map[string]int // Usage calls per account
	Expired   map[string]bool
	CredCalls map[string]int
	ModelList map[string][]adapter.Model   // Models per account; missing = error
	Checks    map[string][]adapter.Finding // Check findings per account
}

func NewFake(vendor string) *Fake {
	return &Fake{
		vendor:    vendor,
		Readings:  map[string]adapter.Reading{},
		Errs:      map[string]error{},
		Calls:     map[string]int{},
		Expired:   map[string]bool{},
		CredCalls: map[string]int{},
		ModelList: map[string][]adapter.Model{},
		Checks:    map[string][]adapter.Finding{},
	}
}

func (f *Fake) Vendor() string { return f.vendor }

func (f *Fake) Credential(acct adapter.Account) adapter.CredStatus {
	f.CredCalls[acct.Name]++
	return adapter.CredStatus{Present: true, Expired: f.Expired[acct.Name], Source: "fake: " + acct.Name}
}

func (f *Fake) Usage(_ context.Context, acct adapter.Account) (adapter.Reading, error) {
	f.Calls[acct.Name]++
	if err, ok := f.Errs[acct.Name]; ok {
		return adapter.Reading{}, err
	}
	if r, ok := f.Readings[acct.Name]; ok {
		return r, nil
	}
	return adapter.Reading{}, adapter.ErrShape
}

func (f *Fake) Models(_ context.Context, acct adapter.Account) ([]adapter.Model, error) {
	if m, ok := f.ModelList[acct.Name]; ok {
		return m, nil
	}
	return nil, errors.New("fake: no models")
}

func (f *Fake) Check(_ context.Context, acct adapter.Account) []adapter.Finding {
	return f.Checks[acct.Name]
}

func (f *Fake) TotalUsage() int {
	n := 0
	for _, c := range f.Calls {
		n += c
	}
	return n
}

// ClaudeReading reports a 5h and a 7d window, matching a two-value cap so no
// cap-length warning is produced.
func ClaudeReading(session, weekly float64) adapter.Reading {
	return adapter.Reading{TakenAt: Now, Windows: []adapter.Window{
		{Label: "session", Length: 5 * time.Hour, UsedPct: session, ResetsAt: Later},
		{Label: "weekly", Length: 7 * 24 * time.Hour, UsedPct: weekly, ResetsAt: Later},
	}}
}

// NewDeps writes configJSON to a temp file and points MODELPICK_CONFIG and
// XDG_CACHE_HOME at temp paths, so nothing real is read or written.
func NewDeps(t *testing.T, configJSON string, fakes ...*Fake) app.Deps {
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
	return app.Deps{
		Getenv:   func(k string) string { return vars[k] },
		Home:     "/home/u",
		Now:      func() time.Time { return Now },
		Adapters: adapters,
	}
}

// Config is a three-account, three-profile v1 config.
const Config = `{
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

// DoctorFakes: every model listed, every account healthy.
func DoctorFakes() (*Fake, *Fake) {
	claude := NewFake("claude")
	claude.ModelList["claude-main"] = []adapter.Model{{ID: "claude-opus-5-5", Thinking: []string{"low", "high"}}}
	codex := NewFake("codex")
	for _, n := range []string{"codex-main", "codex-two"} {
		codex.ModelList[n] = []adapter.Model{{ID: "gpt-6.1-sol", Thinking: []string{"low", "high"}}}
	}
	return claude, codex
}

// WriteStored stores a reading where doctor's Peek will find it.
func WriteStored(t *testing.T, d app.Deps, account, json string) {
	t.Helper()
	cfg, err := app.LoadConfig(d)
	if err != nil {
		t.Fatal(err)
	}
	st := app.NewStore(d, cfg)
	if err := os.MkdirAll(st.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.Dir, account+".json"), []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
}
