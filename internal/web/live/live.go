// Package live is the real web.Backend: the page's view of config.json,
// the saved usage readings and the doctor, and its operations through the
// app editor. A read never calls a vendor: only Refresh, and one background
// first reading per account that has none (owner, 2026-10-23), do.
package live

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"sync"

	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/core"
	"github.com/meatlover/modelpick/internal/web"
)

// Live serves one config file for one run of the server. It holds the one
// app.Editor of the run (the safety copy is written once per run).
type Live struct {
	d     app.Deps
	path  string
	ed    *app.Editor
	first func(account string) // takes an account's first reading; blocks

	mu      sync.Mutex
	started map[string]bool // accounts whose first reading was started
}

var _ web.Backend = (*Live)(nil)

// New serves the config file at path. Every account without a saved
// reading gets its first reading in the background, now or when it is
// added.
func New(d app.Deps, path string) *Live { return newLive(d, path, nil) }

// newLive takes first readings with first (nil: readFirst), so tests can
// see which accounts it is started for.
func newLive(d app.Deps, path string, first func(string)) *Live {
	l := &Live{d: d, path: path, ed: app.NewEditor(d, path), started: map[string]bool{}}
	l.first = first
	if l.first == nil {
		l.first = l.readFirst
	}
	// The editor already starts this in its own goroutine.
	l.ed.OnAccountAdded = l.maybeFirst
	if snap, err := l.ed.Load(); err == nil && snap.Config != nil {
		for _, n := range sortedNames(snap.Config.Accounts) {
			if l.claim(snap.Config, n) {
				go l.first(n)
			}
		}
	}
	return l
}

// claim reports whether account n gets its first reading now: once per
// run, and only while it has no saved reading.
func (l *Live) claim(cfg *core.Config, n string) bool {
	acct, ok := cfg.Accounts[n]
	if !ok {
		return false
	}
	saved, err := app.NewStore(l.d, cfg).Saved(acct)
	if err != nil || saved.Reading != nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.started[n] {
		return false
	}
	l.started[n] = true
	return true
}

// maybeFirst takes an added account's first reading unless it has one.
func (l *Live) maybeFirst(name string) {
	if snap, err := l.ed.Load(); err == nil && snap.Config != nil && l.claim(snap.Config, name) {
		l.first(name)
	}
}

// readFirst is the store's normal read, without refresh: the floor,
// Retry-After and an expired token all hold. The store keeps the outcome,
// error included; nothing is returned.
func (l *Live) readFirst(name string) {
	snap, err := l.ed.Load()
	if err != nil || snap.Config == nil {
		return
	}
	acct, a, err := app.NewEnv(l.d, snap.Config, false).AdapterFor(name)
	if err != nil {
		return
	}
	app.NewStore(l.d, snap.Config).Get(context.Background(), a, acct, false)
}

func sortedNames[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// unreadable is why a file that cannot be read is read-only.
func (l *Live) unreadable(err error) string {
	if errors.Is(err, fs.ErrNotExist) {
		return "config.json not found at " + l.path
	}
	return err.Error()
}

// State is the config as the page needs it, recomputed on every call.
func (l *Live) State() web.State {
	snap, err := l.ed.Load()
	if err != nil {
		sum := sha256.Sum256(nil)
		return emptyState(l.path, hex.EncodeToString(sum[:]), l.unreadable(err))
	}
	s := emptyState(l.path, snap.SHA256, snap.ReadOnly)
	if snap.Doc == nil {
		return s
	}
	s.DefaultCap = snap.Doc.Settings["default_cap"]
	var sources map[string]string
	if cfg := snap.Config; cfg != nil {
		s.DefaultCap = cfg.Settings.DefaultCap
		sources = l.sources(cfg)
		s.Models, s.Windows = l.modelsAndWindows(cfg)
	}
	s.Accounts = wireAccounts(snap.Doc, sources)
	s.Schedules = wireSchedules(snap.Doc)
	s.Profiles = wireProfiles(snap.Doc)
	return s
}

// sources is each account's CredStatus.Source: local only, never a secret.
func (l *Live) sources(cfg *core.Config) map[string]string {
	env := app.NewEnv(l.d, cfg, false)
	out := map[string]string{}
	for n := range cfg.Accounts {
		out[n] = env.Credential(n).Source
	}
	return out
}

// modelsAndWindows lists each account's models (cached until an account
// operation) and, for an account with a saved reading, the window labels
// each model's caps map to (contract 4: absent without a reading).
func (l *Live) modelsAndWindows(cfg *core.Config) (map[string][]web.Model, map[string]map[string][]string) {
	env := app.NewEnv(l.d, cfg, false)
	store := app.NewStore(l.d, cfg)
	models := map[string][]web.Model{}
	windows := map[string]map[string][]string{}
	for name, acct := range cfg.Accounts {
		ids := choiceModels(cfg, name)
		models[name] = []web.Model{}
		if _, a, err := env.AdapterFor(name); err == nil {
			if ms, err := l.ed.Checks().Models(acct, a); err == nil {
				models[name] = wireModels(ms)
				for _, m := range ms {
					ids[m.ID] = true
				}
			}
		}
		saved, err := store.Saved(acct)
		if err != nil || saved.Reading == nil || len(ids) == 0 {
			continue
		}
		windows[name] = map[string][]string{}
		for id := range ids {
			windows[name][id] = windowLabels(saved.Reading.Windows, id)
		}
	}
	return models, windows
}

// choiceModels is the set of models the profiles use with account.
func choiceModels(cfg *core.Config, account string) map[string]bool {
	ids := map[string]bool{}
	for _, cs := range cfg.Profiles {
		for _, c := range cs {
			if c.Account == account {
				ids[c.Model] = true
			}
		}
	}
	return ids
}

// Usage is every account's saved state: no lock, no call.
func (l *Live) Usage() []web.UsageRow {
	rows := []web.UsageRow{}
	snap, err := l.ed.Load()
	if err != nil || snap.Config == nil {
		return rows
	}
	env := app.NewEnv(l.d, snap.Config, false)
	for _, n := range sortedNames(snap.Config.Accounts) {
		rows = append(rows, l.row(snap.Config, env, n))
	}
	return rows
}

// row derives one account's row from its state file and local credential.
// The last error shows while no reading is newer than the failed attempt.
func (l *Live) row(cfg *core.Config, env *app.Env, name string) web.UsageRow {
	acct := cfg.Accounts[name]
	now := l.d.Now()
	saved, err := app.NewStore(l.d, cfg).Saved(acct)
	st := core.AccountState{Reading: saved.Reading, TokenExpired: env.Credential(name).Expired}
	if saved.RetryUntil.After(now) {
		st.RetryUntil = saved.RetryUntil
	}
	switch {
	case err != nil:
		st.Err = err
	case saved.LastError != "" && (saved.Reading == nil || saved.LastAttempt.After(saved.Reading.TakenAt)):
		st.Err = errors.New(saved.LastError)
	}
	return usageRow(name, acct.Vendor, st, now)
}

// Refresh reads the account through the store with refresh=true and
// returns its new row. Refusals lead with the account name (contract 5);
// a failed call is a plain error, which the server reports without its
// detail.
func (l *Live) Refresh(account string) (web.UsageRow, error) {
	snap, err := l.ed.Load()
	if err != nil || snap.Config == nil {
		return web.UsageRow{}, web.Refused{Reason: account + ": not refreshed while config.json is read-only"}
	}
	cfg := snap.Config
	if _, ok := cfg.Accounts[account]; !ok {
		return web.UsageRow{}, web.Refused{Reason: account + ": unknown account"}
	}
	env := app.NewEnv(l.d, cfg, false)
	acct, a, err := env.AdapterFor(account)
	if err != nil {
		return web.UsageRow{}, fmt.Errorf("refresh %s: %w", account, err)
	}
	if row := l.row(cfg, env, account); !row.CanRefresh {
		return web.UsageRow{}, web.Refused{Reason: account + ": " + row.State}
	}
	if res := app.NewStore(l.d, cfg).Get(context.Background(), a, acct, true); res.Err != nil {
		return web.UsageRow{}, fmt.Errorf("refresh %s: %w", account, res.Err)
	}
	return l.row(cfg, app.NewEnv(l.d, cfg, false), account), nil
}

// Doctor re-runs each account's local Check (binary, credential) and
// reuses the model lists (ruling 4). A file that is missing or does not
// parse is its one finding.
func (l *Live) Doctor() web.Doctor {
	snap, err := l.ed.Load()
	switch {
	case err != nil:
		return wireDoctor([]app.Finding{app.ConfigFinding(errors.New(l.unreadable(err)))})
	case snap.Config == nil:
		return wireDoctor([]app.Finding{app.ConfigFinding(errors.New(snap.ReadOnly))})
	}
	l.ed.Checks().ForgetChecks()
	return wireDoctor(app.Doctor(snap.Config, l.d, l.ed.Checks()))
}

// Apply runs one operation through the editor's gate.
func (l *Live) Apply(req web.OpRequest) (web.OpResult, error) {
	snap, findings, err := l.ed.Apply(req.Base, req.Op, req.Args)
	if err != nil {
		return web.OpResult{}, l.opError(err)
	}
	return web.OpResult{SHA256: snap.SHA256, Findings: wireFindings(findings)}, nil
}

// opError maps the editor's errors to the wire's; anything else is an
// internal error the server does not show.
func (l *Live) opError(err error) error {
	var c *app.Conflict
	var r *app.Refused
	switch {
	case errors.As(err, &c):
		return web.Conflict{SHA256: c.SHA256}
	case errors.As(err, &r):
		return web.Refused{Reason: r.Reason, Uses: r.Uses}
	case errors.Is(err, fs.ErrNotExist):
		return web.Refused{Reason: l.unreadable(err)}
	}
	return err
}
