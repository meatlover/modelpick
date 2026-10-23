package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/core"
	"github.com/meatlover/modelpick/internal/platform"
	"github.com/meatlover/modelpick/internal/usage"
)

// toolNames are the vendors' CLI names as users know them, for advice in
// error messages.
var toolNames = map[string]string{
	"claude":      "Claude Code",
	"codex":       "Codex",
	"opencode-go": "OpenCode",
}

// env implements core.Env over Deps and the usage store. It reads each
// account's credential and usage at most once.
type env struct {
	d       Deps
	cfg     *core.Config
	store   *usage.Store
	refresh bool
	creds   map[string]adapter.CredStatus
	states  map[string]core.AccountState
}

func newEnv(d Deps, cfg *core.Config, refresh bool) *env {
	return &env{
		d:       d,
		cfg:     cfg,
		store:   newStore(d, cfg),
		refresh: refresh,
		creds:   map[string]adapter.CredStatus{},
		states:  map[string]core.AccountState{},
	}
}

// newStore is the usage store for cfg: one state file per account under the
// cache dir, at most one vendor call per account per floor.
func newStore(d Deps, cfg *core.Config) *usage.Store {
	return &usage.Store{
		Dir:         platform.CacheDir(d.Getenv, d.Home),
		Floor:       cfg.Settings.MinCallInterval,
		Now:         d.Now,
		CallTimeout: 10 * time.Second,
	}
}

// adapterFor returns the account and its vendor's adapter.
func (e *env) adapterFor(name string) (adapter.Account, adapter.Adapter, error) {
	acct := e.cfg.Accounts[name]
	a, ok := e.d.Adapters[acct.Vendor]
	if !ok {
		return acct, nil, fmt.Errorf("no adapter for vendor %q", acct.Vendor)
	}
	return acct, a, nil
}

// Credential reports the account's local credential; no network.
func (e *env) Credential(name string) adapter.CredStatus {
	if c, ok := e.creds[name]; ok {
		return c
	}
	acct, a, err := e.adapterFor(name)
	c := adapter.CredStatus{Err: err}
	if err == nil {
		c = a.Credential(acct)
	}
	e.creds[name] = c
	return c
}

// Usage returns the account's reading through the store (floor, cache,
// Retry-After), with errors rewritten as advice.
func (e *env) Usage(name string) core.AccountState {
	if st, ok := e.states[name]; ok {
		return st
	}
	acct, a, err := e.adapterFor(name)
	st := core.AccountState{Err: err}
	if err == nil {
		res := e.store.Get(context.Background(), a, acct, e.refresh)
		st = core.AccountState{Reading: res.Reading, Warnings: res.Warnings,
			TokenExpired: res.TokenExpired, RetryUntil: res.RetryUntil}
		if res.Err != nil {
			st.Err = usageError(acct.Vendor, res.Err)
		}
	}
	e.states[name] = st
	return st
}

// usageError rewrites err as what the user should do about it, keeping err
// matchable with errors.Is.
func usageError(vendor string, err error) error {
	tool, ok := toolNames[vendor]
	if !ok {
		tool = vendor
	}
	switch {
	case errors.Is(err, adapter.ErrUnauthorized):
		return &adviceError{"usage endpoint returned HTTP 401; log in to " + tool + " with this account", err}
	case errors.Is(err, adapter.ErrTokenExpired):
		return &adviceError{"token expired; open " + tool + " with this account to refresh it", err}
	}
	return err
}

// adviceError replaces an error's text while keeping it as the cause.
type adviceError struct {
	msg   string
	cause error
}

func (e *adviceError) Error() string { return e.msg }
func (e *adviceError) Unwrap() error { return e.cause }
