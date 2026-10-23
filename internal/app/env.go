package app

import (
	"context"
	"fmt"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/core"
	"github.com/meatlover/modelpick/internal/platform"
	"github.com/meatlover/modelpick/internal/usage"
)

// Env implements core.Env over Deps and the usage store. It reads each
// account's credential and usage at most once.
type Env struct {
	d       Deps
	cfg     *core.Config
	store   *usage.Store
	refresh bool
	creds   map[string]adapter.CredStatus
	states  map[string]core.AccountState
}

// NewEnv builds an Env for cfg; refresh bypasses the store's floor.
func NewEnv(d Deps, cfg *core.Config, refresh bool) *Env {
	return &Env{
		d:       d,
		cfg:     cfg,
		store:   NewStore(d, cfg),
		refresh: refresh,
		creds:   map[string]adapter.CredStatus{},
		states:  map[string]core.AccountState{},
	}
}

// NewStore is the usage store for cfg: one state file per account under the
// cache dir, at most one vendor call per account per floor.
func NewStore(d Deps, cfg *core.Config) *usage.Store {
	return &usage.Store{
		Dir:         platform.CacheDir(d.Getenv, d.Home),
		Floor:       cfg.Settings.MinCallInterval,
		Now:         d.Now,
		CallTimeout: 10 * time.Second,
	}
}

// AdapterFor returns the account and its vendor's adapter.
func (e *Env) AdapterFor(name string) (adapter.Account, adapter.Adapter, error) {
	acct := e.cfg.Accounts[name]
	a, ok := e.d.Adapters[acct.Vendor]
	if !ok {
		return acct, nil, fmt.Errorf("no adapter for vendor %q", acct.Vendor)
	}
	return acct, a, nil
}

// Credential reports the account's local credential; no network.
func (e *Env) Credential(name string) adapter.CredStatus {
	if c, ok := e.creds[name]; ok {
		return c
	}
	acct, a, err := e.AdapterFor(name)
	c := adapter.CredStatus{Err: err}
	if err == nil {
		c = a.Credential(acct)
	}
	e.creds[name] = c
	return c
}

// Usage returns the account's reading through the store (floor, cache,
// Retry-After), with errors rewritten as advice.
func (e *Env) Usage(name string) core.AccountState {
	if st, ok := e.states[name]; ok {
		return st
	}
	acct, a, err := e.AdapterFor(name)
	st := core.AccountState{Err: err}
	if err == nil {
		res := e.store.Get(context.Background(), a, acct, e.refresh)
		st = core.AccountState{Reading: res.Reading, Warnings: res.Warnings,
			TokenExpired: res.TokenExpired, RetryUntil: res.RetryUntil}
		if res.Err != nil {
			st.Err = UsageError(acct.Vendor, res.Err)
		}
	}
	e.states[name] = st
	return st
}
