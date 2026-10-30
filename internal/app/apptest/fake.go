// Package apptest holds the fakes shared by the test suites of app and its
// callers: a canned vendor adapter and a Deps that touches nothing real.
package apptest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
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

// Fake serves canned usage per account name and counts calls. Its maps are
// set directly before any call may run; once one may, Errs, Checks and
// ModelList change only through SetErr, SetChecks and SetModels, which take
// the lock their readers take. Readings and Expired must not be mutated
// then. An account missing from Readings and Errs has unknown usage
// (ErrShape).
type Fake struct {
	vendor   string
	Readings map[string]adapter.Reading
	Errs     map[string]error
	Calls    map[string]int // Usage calls per account
	Expired  map[string]bool
	// LoginExpires is the refresh token's expiry Credential reports per
	// account; missing = unknown (zero).
	LoginExpires map[string]time.Time
	CredCalls    map[string]int
	ModelList    map[string][]adapter.Model   // Models per account; missing = error
	Checks       map[string][]adapter.Finding // Check findings per account
	CheckCalls   map[string]int               // Check calls per account
	ModelCalls   map[string]int               // Models calls per account
	ModelDelay   time.Duration                // each Models call takes this long, or until its ctx ends
	ModelPanic   map[string]bool              // Models panics for these accounts
	ModelDone    map[string]int               // Models calls that ran to the end (not cancelled)
	ModelEnded   map[string]int               // Models calls that returned, cancelled or not
	UsageDelay   time.Duration                // each Usage call takes this long, or until its ctx ends
	UsageSlow    time.Duration                // a Usage call its ctx ended takes this long more to return
	usageEnded   int                          // Usage calls that returned, cancelled or not
	// UsageTogether makes each Usage call wait until that many are in
	// progress at once, or togetherWait passes; a call that gave up is
	// counted in TogetherMissed. Calls made one after another never meet,
	// so a test of concurrency needs no wall-clock bound.
	UsageTogether  int
	inFlight       int
	together       chan struct{} // closed when UsageTogether calls are in progress
	togetherMissed int

	// Renewable makes the vendor's adapter in NewDeps implement
	// adapter.Renewer (any entry, so an empty map stays non-renewing); an
	// account listed false fails Renew. A successful Renew clears Expired.
	Renewable  map[string]bool
	RenewCalls map[string]int   // Renew calls per account
	RenewErr   map[string]error // Renew fails with this error

	mu sync.Mutex // guards the call counters, Errs, Checks and ModelList: calls run concurrently
}

// SetErr makes account's Usage calls fail with err; nil removes the failure.
func (f *Fake) SetErr(account string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		delete(f.Errs, account)
		return
	}
	f.Errs[account] = err
}

// SetChecks sets account's Check findings; nil removes them.
func (f *Fake) SetChecks(account string, fs []adapter.Finding) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if fs == nil {
		delete(f.Checks, account)
		return
	}
	f.Checks[account] = fs
}

// SetModels sets account's model list; nil removes it, so Models fails.
func (f *Fake) SetModels(account string, ms []adapter.Model) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ms == nil {
		delete(f.ModelList, account)
		return
	}
	f.ModelList[account] = ms
}

// ModelCallCount is the Models calls for account, safe beside running calls.
func (f *Fake) ModelCallCount(account string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ModelCalls[account]
}

func NewFake(vendor string) *Fake {
	return &Fake{
		vendor:       vendor,
		Readings:     map[string]adapter.Reading{},
		Errs:         map[string]error{},
		Calls:        map[string]int{},
		Expired:      map[string]bool{},
		LoginExpires: map[string]time.Time{},
		CredCalls:    map[string]int{},
		ModelList:    map[string][]adapter.Model{},
		Checks:       map[string][]adapter.Finding{},
		CheckCalls:   map[string]int{},
		ModelCalls:   map[string]int{},
		ModelDone:    map[string]int{},
		ModelEnded:   map[string]int{},
		Renewable:    map[string]bool{},
		RenewCalls:   map[string]int{},
		RenewErr:     map[string]error{},
	}
}

// renewingFake is a Fake that is also an adapter.Renewer. NewDeps wraps a
// Fake in it only when Renewable is set, so a plain Fake stays non-renewing.
type renewingFake struct{ *Fake }

// Renew counts the call, then fails or clears the account's Expired flag.
// Like a real renewal it ends with its ctx.
func (r renewingFake) Renew(ctx context.Context, acct adapter.Account) error {
	f := r.Fake
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RenewCalls[acct.Name]++
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := f.RenewErr[acct.Name]; err != nil {
		return err
	}
	if !f.Renewable[acct.Name] {
		return &adapter.RenewError{Reason: "not renewable"}
	}
	delete(f.Expired, acct.Name)
	return nil
}

func (f *Fake) Vendor() string { return f.vendor }

func (f *Fake) Credential(acct adapter.Account) adapter.CredStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.CredCalls[acct.Name]++
	return adapter.CredStatus{Present: true, Expired: f.Expired[acct.Name], LoginExpires: f.LoginExpires[acct.Name], Source: "fake: " + acct.Name}
}

func (f *Fake) Usage(ctx context.Context, acct adapter.Account) (adapter.Reading, error) {
	f.mu.Lock()
	f.Calls[acct.Name]++
	f.inFlight++
	var together chan struct{}
	if f.UsageTogether > 0 {
		if f.together == nil {
			f.together = make(chan struct{})
		}
		if f.inFlight == f.UsageTogether {
			close(f.together)
		}
		together = f.together
	}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.usageEnded++
		f.inFlight--
		f.mu.Unlock()
	}()
	if together != nil {
		select {
		case <-together:
		case <-time.After(togetherWait):
			f.mu.Lock()
			f.togetherMissed++
			f.mu.Unlock()
		case <-ctx.Done():
			return adapter.Reading{}, ctx.Err()
		}
	}
	if f.UsageDelay > 0 {
		select {
		case <-time.After(f.UsageDelay):
		case <-ctx.Done():
			time.Sleep(f.UsageSlow)
			return adapter.Reading{}, ctx.Err()
		}
	}
	f.mu.Lock()
	err, failed := f.Errs[acct.Name]
	f.mu.Unlock()
	if failed {
		return adapter.Reading{}, err
	}
	if r, ok := f.Readings[acct.Name]; ok {
		return r, nil
	}
	return adapter.Reading{}, adapter.ErrShape
}

func (f *Fake) Models(ctx context.Context, acct adapter.Account) ([]adapter.Model, error) {
	f.mu.Lock()
	f.ModelCalls[acct.Name]++
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.ModelEnded[acct.Name]++
		f.mu.Unlock()
	}()
	if f.ModelPanic[acct.Name] {
		panic("fake: Models panics for " + acct.Name)
	}
	select {
	case <-time.After(f.ModelDelay):
	case <-ctx.Done():
		time.Sleep(100 * time.Millisecond) // a cancelled call is slow to unwind
		return nil, ctx.Err()
	}
	f.mu.Lock()
	f.ModelDone[acct.Name]++
	m, ok := f.ModelList[acct.Name]
	f.mu.Unlock()
	if ok {
		return m, nil
	}
	return nil, errors.New("fake: no models")
}

func (f *Fake) Check(_ context.Context, acct adapter.Account) []adapter.Finding {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.CheckCalls[acct.Name]++
	return f.Checks[acct.Name]
}

// TotalUsage is the Usage calls over all accounts, safe beside running calls.
func (f *Fake) TotalUsage() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.Calls {
		n += c
	}
	return n
}

// togetherWait bounds how long a Usage call waits for UsageTogether calls.
const togetherWait = 5 * time.Second

// TogetherMissed is the Usage calls that stopped waiting for UsageTogether
// calls to be in progress at once; safe beside running calls.
func (f *Fake) TogetherMissed() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.togetherMissed
}

// UsageEnded is the Usage calls over all accounts that have returned,
// cancelled or not; safe beside running calls.
func (f *Fake) UsageEnded() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.usageEnded
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
	d, err := NewDepsDir(t.TempDir(), configJSON, fakes...)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// NewDepsDir is NewDeps in dir: it writes dir/config.json and points
// MODELPICK_CONFIG at it and XDG_CACHE_HOME at dir/cache. The clock is Now,
// and the shuffle leaves load balancing groups in file order.
func NewDepsDir(dir, configJSON string, fakes ...*Fake) (app.Deps, error) {
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, []byte(configJSON), 0o600); err != nil {
		return app.Deps{}, err
	}
	vars := map[string]string{
		"MODELPICK_CONFIG": cfgPath,
		"XDG_CACHE_HOME":   filepath.Join(dir, "cache"),
	}
	adapters := map[string]adapter.Adapter{}
	for _, f := range fakes {
		adapters[f.vendor] = f
		if len(f.Renewable) > 0 {
			adapters[f.vendor] = renewingFake{f}
		}
	}
	return app.Deps{
		Getenv:   func(k string) string { return vars[k] },
		Home:     "/home/u",
		Now:      func() time.Time { return Now },
		Adapters: adapters,
		// Load balancing groups keep file order, so picks are deterministic.
		Shuffle: func(int, func(i, j int)) {},
	}, nil
}

// Config is a three-account, three-profile v1 config.
const Config = `{
  "version": 1,
  "accounts": {
    "claude-main": { "vendor": "claude", "config_dir": "~/.claude" },
    "claude-secondary":  { "vendor": "claude", "config_dir": "~/.claude-secondary" },
    "codex-main":  { "vendor": "codex", "codex_home": "~/.codex" }
  },
  "profiles": {
    "two-claude": [
      { "account": "claude-main", "model": "claude-opus-5-5", "thinking": "high", "cap": [90, 98] },
      { "account": "claude-secondary", "model": "claude-sonnet-5-5", "thinking": "medium" }
    ],
    "codex-first": [
      { "account": "codex-main", "model": "gpt-6.1-sol", "thinking": "high", "cap": [98] },
      { "account": "claude-secondary", "model": "claude-sonnet-5-5", "thinking": "medium" }
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

// WriteStored stores a reading where the store's Saved will find it.
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

// ModelDoneCount is the Models calls of account that ran to the end.
func (f *Fake) ModelDoneCount(account string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ModelDone[account]
}

// ModelEndedCount is the Models calls of account that have returned.
func (f *Fake) ModelEndedCount(account string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ModelEnded[account]
}
