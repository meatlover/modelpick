package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/core"
)

type accountCheck struct {
	findings []adapter.Finding
	// loginExpires is the login's expiry as of the cached Check; zero = unknown.
	loginExpires time.Time
	models       []adapter.Model
	modelsErr    error
}

// checkEntry is one account's cached results. Each part has its own lock,
// so a slow Check does not hold up the model list and neither holds up
// another account.
type checkEntry struct {
	acct adapter.Account // the account as configured when the entry was made

	checkMu  sync.Mutex
	checked  bool
	stale    atomic.Bool // set by ForgetChecks: run Check again on next use
	findings []adapter.Finding
	// loginExpires is read with the Check, so both go stale together.
	loginExpires time.Time

	modelsMu  sync.Mutex
	relist    atomic.Bool // set by ForgetChecks: list again on next use if the listing failed
	listed    bool
	models    []adapter.Model
	modelsErr error
}

// CheckCache keeps each account's adapter Check findings, login expiry and Models result
// until forgotten, so repeated doctor runs do not repeat vendor-side work.
// The zero value is ready; it is safe for concurrent use, and its own
// mutex is never held across an adapter call.
type CheckCache struct {
	mu sync.Mutex
	m  map[string]*checkEntry
}

// entry is the account's entry. One made for another vendor or locator
// under the same name (a hand edit while the page is open) is replaced, so
// it never answers for the account as it is now.
func (c *CheckCache) entry(acct adapter.Account) *checkEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]*checkEntry{}
	}
	e, ok := c.m[acct.Name]
	if !ok || e.acct != acct {
		e = &checkEntry{acct: acct}
		c.m[acct.Name] = e
	}
	return e
}

// Forget drops one account's cached results.
func (c *CheckCache) Forget(account string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, account)
}

// ForgetChecks makes the next use of each account run its Check again
// (binary, credential: local state that changes while the page is open)
// and list its models again if that listing failed (a timeout, opencode
// not installed yet), like a cold listing. A listing that succeeded stays
// until the account is forgotten.
func (c *CheckCache) ForgetChecks() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.m {
		e.stale.Store(true)
		e.relist.Store(true)
	}
}

// Models returns the account's model list, listing it once until forgotten
// (or, for a failed listing, until ForgetChecks).
func (c *CheckCache) Models(acct adapter.Account, a adapter.Adapter) ([]adapter.Model, error) {
	return c.ModelsCtx(context.Background(), acct, a)
}

// ModelsCtx is Models that gives up when ctx ends. A listing cut short by
// ctx is not cached, so the next use lists again.
func (c *CheckCache) ModelsCtx(ctx context.Context, acct adapter.Account, a adapter.Adapter) ([]adapter.Model, error) {
	e := c.entry(acct)
	e.modelsMu.Lock()
	defer e.modelsMu.Unlock()
	if relist := e.relist.Swap(false); !e.listed || (relist && e.modelsErr != nil) {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		ms, err, panicked := callModels(cctx, acct, a)
		cancel()
		if panicked {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		e.models, e.modelsErr, e.listed = ms, err, true
	}
	return e.models, e.modelsErr
}

// WarmModels lists the models of the named accounts of cfg concurrently,
// so a cold request waits for the slowest account, not the sum. Accounts
// with no adapter are skipped; their error shows where the list is used.
func (c *CheckCache) WarmModels(ctx context.Context, d Deps, cfg *core.Config, names []string) {
	env := NewEnv(d, cfg, false)
	var wg sync.WaitGroup
	for _, n := range names {
		acct, a, err := env.AdapterFor(n)
		if err != nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.ModelsCtx(ctx, acct, a)
		}()
	}
	wg.Wait()
}

func (c *CheckCache) checkFindings(acct adapter.Account, a adapter.Adapter) ([]adapter.Finding, time.Time) {
	e := c.entry(acct)
	e.checkMu.Lock()
	defer e.checkMu.Unlock()
	if stale := e.stale.Swap(false); !e.checked || stale {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		f, ok := callCheck(ctx, acct, a)
		cancel()
		e.findings, e.checked = f, ok
		e.loginExpires = time.Time{}
		if ok {
			e.loginExpires = callLoginExpires(acct, a)
		}
	}
	return e.findings, e.loginExpires
}

// check returns the account's cached Check findings and model list. name
// is the config's key for the account (acct.Name may be unset).
func (c *CheckCache) check(name string, acct adapter.Account, a adapter.Adapter) accountCheck {
	acct.Name = name
	var r accountCheck
	r.findings, r.loginExpires = c.checkFindings(acct, a)
	r.models, r.modelsErr = c.Models(acct, a)
	return r
}

// warmChecks runs the Check and Models of the named accounts concurrently;
// the results stay in the cache for the sequential reading that follows.
func (c *CheckCache) warmChecks(e *Env, names []string) {
	var wg sync.WaitGroup
	for _, n := range names {
		acct, a, err := e.AdapterFor(n)
		if err != nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.check(n, acct, a)
		}()
	}
	wg.Wait()
}

// errLog receives problems the page is not shown: adapter panics and a
// removed account's state files that could not be deleted.
var errLog = log.New(os.Stderr, "modelpick: ", log.LstdFlags)

// callModels is a.Models turning a panic into an error (panicked), so one broken
// adapter fails its own listing (not cached) instead of the server.
func callModels(ctx context.Context, acct adapter.Account, a adapter.Adapter) (ms []adapter.Model, err error, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			errLog.Printf("%s: listing models panicked: %v", acct.Name, r)
			ms, err, panicked = nil, fmt.Errorf("listing models panicked: %v", r), true
		}
	}()
	ms, err = a.Models(ctx, acct)
	return ms, err, false
}

// callLoginExpires is the refresh token expiry a.Credential reports (local
// only), zero when unknown or when the adapter panics.
func callLoginExpires(acct adapter.Account, a adapter.Adapter) (t time.Time) {
	defer func() {
		if r := recover(); r != nil {
			errLog.Printf("%s: reading the credential panicked: %v", acct.Name, r)
			t = time.Time{}
		}
	}()
	return a.Credential(acct).LoginExpires
}

// callCheck is a.Check turning a panic into no findings; ok false keeps
// the result out of the cache so the next use runs Check again.
func callCheck(ctx context.Context, acct adapter.Account, a adapter.Adapter) (f []adapter.Finding, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			errLog.Printf("%s: check panicked: %v", acct.Name, r)
			f, ok = nil, false
		}
	}()
	return a.Check(ctx, acct), true
}
