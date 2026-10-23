package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/core"
)

const (
	sevError   = "error"
	sevWarning = "warning"
)

// Finding is one doctor result. Subject is `<account>`, `profile <p>`,
// `profile <p>, choice <n>` (1-based), `schedule <name>` or `config`. The UI
// compares findings by subject and message, so neither carries a time or a
// counter.
type Finding struct{ Severity, Subject, Message string }

// ConfigFinding is the one finding of a config that does not parse.
func ConfigFinding(err error) Finding {
	return Finding{Severity: sevError, Subject: "config", Message: err.Error()}
}

type accountCheck struct {
	findings  []adapter.Finding
	models    []adapter.Model
	modelsErr error
}

// CheckCache keeps each account's adapter Check findings and Models result
// until forgotten, so repeated doctor runs do not repeat vendor-side work.
// The zero value is ready; it is safe for concurrent use.
type CheckCache struct {
	mu sync.Mutex
	m  map[string]accountCheck
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

// check returns the account's cached results, running Check and Models once.
func (c *CheckCache) check(name string, acct adapter.Account, a adapter.Adapter) accountCheck {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r, ok := c.m[name]; ok {
		return r
	}
	var r accountCheck
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	r.findings = a.Check(ctx, acct)
	cancel()
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	r.models, r.modelsErr = a.Models(ctx, acct)
	cancel()
	if c.m == nil {
		c.m = map[string]accountCheck{}
	}
	c.m[name] = r
	return r
}

// findings collects findings, dropping exact repeats. fromCheck marks the
// ones an adapter's Check reported: machine state, which the editor's gate
// does not compare.
type findings struct {
	list      []Finding
	seen      map[Finding]bool
	fromCheck map[Finding]bool
}

func (f *findings) add(sev, subject, msg string) {
	x := Finding{sev, subject, msg}
	if f.seen[x] {
		return
	}
	if f.seen == nil {
		f.seen = map[Finding]bool{}
	}
	f.seen[x] = true
	f.list = append(f.list, x)
}

// Doctor validates a parsed config and what it depends on. It never calls
// Usage or any vendor's usage endpoint and never takes the store lock; the
// per-account Check and Models results come from cache (nil = a fresh one).
// The result is sorted by severity (errors first), subject, message.
func Doctor(cfg *core.Config, d Deps, cache *CheckCache) []Finding {
	return doctor(cfg, d, cache).list
}

// doctor is Doctor keeping which findings came from an adapter's Check.
func doctor(cfg *core.Config, d Deps, cache *CheckCache) findings {
	if cache == nil {
		cache = &CheckCache{}
	}
	var f findings
	accounts := make([]string, 0, len(cfg.Accounts))
	for n := range cfg.Accounts {
		accounts = append(accounts, n)
	}
	sort.Strings(accounts)

	models := accountFindings(&f, cfg, d, cache, accounts)
	savedFindings(&f, cfg, d, accounts)
	scheduleFindings(cfg, f.add)
	for _, p := range sortedProfiles(cfg) {
		for i, c := range cfg.Profiles[p] {
			subject := fmt.Sprintf("profile %s, choice %d", p, i+1)
			if ms, ok := models[c.Account]; ok {
				thinkingFinding(&f, subject, c, ms)
			}
		}
		neverFindings(cfg, p, f.add)
	}
	sortFindings(f.list)
	return f
}

func sortFindings(fs []Finding) {
	rank := func(s string) int {
		if s == sevError {
			return 0
		}
		return 1
	}
	sort.SliceStable(fs, func(a, b int) bool {
		x, y := fs[a], fs[b]
		if rank(x.Severity) != rank(y.Severity) {
			return rank(x.Severity) < rank(y.Severity)
		}
		if x.Subject != y.Subject {
			return x.Subject < y.Subject
		}
		return x.Message < y.Message
	})
}

// accountFindings reports each account's adapter Check findings and returns
// the model list of every account whose models could be listed.
func accountFindings(f *findings, cfg *core.Config, d Deps, cache *CheckCache, accounts []string) map[string][]adapter.Model {
	e := NewEnv(d, cfg, false)
	models := map[string][]adapter.Model{}
	for _, n := range accounts {
		acct, a, err := e.AdapterFor(n)
		if err != nil {
			f.add(sevError, n, err.Error())
			continue
		}
		r := cache.check(n, acct, a)
		for _, x := range r.findings {
			subject := x.Account
			if subject == "" {
				subject = n
			}
			f.add(string(x.Severity), subject, x.Message)
			if f.fromCheck == nil {
				f.fromCheck = map[Finding]bool{}
			}
			f.fromCheck[Finding{string(x.Severity), subject, x.Message}] = true
		}
		if r.modelsErr != nil {
			f.add(sevWarning, n, "cannot list models, thinking values not checked: "+UsageError(acct.Vendor, r.modelsErr).Error())
			continue
		}
		models[n] = r.models // present even when empty, so its choices are still checked
	}
	return models
}

// savedFindings compares each capped choice's cap count with the windows of
// the account's saved reading; no lock, no network.
func savedFindings(f *findings, cfg *core.Config, d Deps, accounts []string) {
	store := NewStore(d, cfg)
	saved := map[string]*adapter.Reading{}
	for _, n := range accounts {
		s, err := store.Saved(cfg.Accounts[n])
		if err != nil {
			f.add(sevWarning, n, "cannot read stored usage: "+err.Error())
			continue
		}
		saved[n] = s.Reading
		if s.Reading != nil {
			for _, note := range s.Reading.Notes {
				f.add(sevWarning, n, note)
			}
		}
	}
	for _, p := range sortedProfiles(cfg) {
		for i, c := range cfg.Profiles[p] {
			rd := saved[c.Account]
			if !c.Capped || rd == nil {
				continue
			}
			ordered := core.OrderWindows(rd.Windows, c.Model)
			_, warns := core.EffectiveCaps(c.Account, c.Cap, ordered, cfg.Settings.DefaultCap)
			for _, w := range warns {
				f.add(sevWarning, fmt.Sprintf("profile %s, choice %d", p, i+1), w)
			}
		}
	}
}

// thinkingFinding checks one choice's model and thinking value against the
// account's model list. A null thinking is always fine.
func thinkingFinding(f *findings, subject string, c core.Choice, ms []adapter.Model) {
	var m *adapter.Model
	for j := range ms {
		if ms[j].ID == c.Model {
			m = &ms[j]
			break
		}
	}
	if m == nil {
		f.add(sevWarning, subject, fmt.Sprintf("model %s not offered by %s", c.Model, c.Account))
		return
	}
	if c.Thinking == nil {
		return
	}
	for _, t := range m.Thinking {
		if t == *c.Thinking {
			return
		}
	}
	supported := "none"
	if len(m.Thinking) > 0 {
		supported = strings.Join(m.Thinking, ", ")
	}
	f.add(sevError, subject, fmt.Sprintf("thinking %q not supported by %s (supported: %s)", *c.Thinking, c.Model, supported))
}
