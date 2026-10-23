package app

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
)

type accountCheck struct {
	findings  []adapter.Finding
	models    []adapter.Model
	modelsErr error
}

// checkEntry is one account's cached results. Each part has its own lock,
// so a slow Check does not hold up the model list and neither holds up
// another account.
type checkEntry struct {
	checkMu  sync.Mutex
	checked  bool
	stale    atomic.Bool // set by ForgetChecks: run Check again on next use
	findings []adapter.Finding

	modelsMu  sync.Mutex
	listed    bool
	models    []adapter.Model
	modelsErr error
}

// CheckCache keeps each account's adapter Check findings and Models result
// until forgotten, so repeated doctor runs do not repeat vendor-side work.
// The zero value is ready; it is safe for concurrent use, and its own
// mutex is never held across an adapter call.
type CheckCache struct {
	mu sync.Mutex
	m  map[string]*checkEntry
}

func (c *CheckCache) entry(name string) *checkEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]*checkEntry{}
	}
	e, ok := c.m[name]
	if !ok {
		e = &checkEntry{}
		c.m[name] = e
	}
	return e
}

// Forget drops one account's cached results.
func (c *CheckCache) Forget(account string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, account)
}

// ForgetAll drops every cached result.
func (c *CheckCache) ForgetAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m = nil
}

// ForgetChecks makes the next use of each account run its Check again
// (binary, credential: local state that changes while the page is open)
// and keeps the model lists until an account is forgotten.
func (c *CheckCache) ForgetChecks() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.m {
		e.stale.Store(true)
	}
}

// Models returns the account's model list, listing it once until forgotten.
func (c *CheckCache) Models(acct adapter.Account, a adapter.Adapter) ([]adapter.Model, error) {
	e := c.entry(acct.Name)
	e.modelsMu.Lock()
	defer e.modelsMu.Unlock()
	if !e.listed {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		e.models, e.modelsErr = a.Models(ctx, acct)
		cancel()
		e.listed = true
	}
	return e.models, e.modelsErr
}

func (c *CheckCache) checkFindings(acct adapter.Account, a adapter.Adapter) []adapter.Finding {
	e := c.entry(acct.Name)
	e.checkMu.Lock()
	defer e.checkMu.Unlock()
	if stale := e.stale.Swap(false); !e.checked || stale {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		e.findings = a.Check(ctx, acct)
		cancel()
		e.checked = true
	}
	return e.findings
}

// check returns the account's cached Check findings and model list. name
// is the config's key for the account (acct.Name may be unset).
func (c *CheckCache) check(name string, acct adapter.Account, a adapter.Adapter) accountCheck {
	acct.Name = name
	var r accountCheck
	r.findings = c.checkFindings(acct, a)
	r.models, r.modelsErr = c.Models(acct, a)
	return r
}
