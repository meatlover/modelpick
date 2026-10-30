package app

import (
	"fmt"
	"sort"
	"strings"
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
// counter. Accounts is set only on a grouped finding (one cause shared by
// two or more accounts): their names, sorted, with Subject their join.
type Finding struct {
	Severity, Subject, Message string
	Accounts                   []string
}

// findingKey is what identifies a finding: Finding holds a slice, so it is
// not comparable and cannot key a map itself.
type findingKey struct{ sev, subject, msg string }

func (x Finding) key() findingKey { return findingKey{x.Severity, x.Subject, x.Message} }

// ConfigFinding is the one finding of a config that does not parse.
func ConfigFinding(err error) Finding {
	return Finding{Severity: sevError, Subject: "config", Message: err.Error()}
}

// findings collects findings, dropping exact repeats. fromCheck marks the
// ones an adapter's Check reported: machine state, which the editor's gate
// does not compare.
type findings struct {
	list      []Finding
	seen      map[findingKey]bool
	fromCheck map[findingKey]bool
}

func (f *findings) add(sev, subject, msg string) {
	x := Finding{Severity: sev, Subject: subject, Message: msg}
	if f.seen[x.key()] {
		return
	}
	if f.seen == nil {
		f.seen = map[findingKey]bool{}
	}
	f.seen[x.key()] = true
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

	models, checked := accountFindings(&f, cfg, d, cache, accounts)
	savedFindings(&f, cfg, d, accounts, checked)
	scheduleFindings(cfg, f.add)
	for _, p := range sortedProfiles(cfg) {
		labels := profileLabels(cfg.Profiles[p])
		for i, c := range cfg.Profiles[p] {
			subject := fmt.Sprintf("profile %s, choice %s", p, labels[i])
			if ms, ok := models[c.Account]; ok {
				thinkingFinding(&f, subject, c, ms)
			}
		}
		neverFindings(cfg, p, f.add)
		identicalMemberFindings(p, cfg.Profiles[p], f.add)
	}
	groupCheckFindings(&f)
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
// the model list of every account whose models could be listed, and the set
// of accounts with a Check finding of any severity.
func accountFindings(f *findings, cfg *core.Config, d Deps, cache *CheckCache, accounts []string) (map[string][]adapter.Model, map[string]bool) {
	e := NewEnv(d, cfg, false)
	models := map[string][]adapter.Model{}
	checked := map[string]bool{}
	cache.warmChecks(e, accounts)
	for _, n := range accounts {
		acct, a, err := e.AdapterFor(n)
		if err != nil {
			f.add(sevError, n, err.Error())
			continue
		}
		r := cache.check(n, acct, a)
		checkErr := checkFindings(f, n, r.findings)
		loginExpiryFinding(f, n, acct.Vendor, r.loginExpires, d.Now(), cfg.Settings.LoginExpiryWarning)
		checked[n] = len(r.findings) > 0
		if r.modelsErr != nil {
			// A Check error (tool or credential missing) is the cause of the
			// failed listing; a second warning would repeat it.
			if !checkErr {
				f.add(sevWarning, n, "cannot list models, thinking values not checked: "+UsageError(acct.Vendor, r.modelsErr).Error())
			}
			continue
		}
		models[n] = r.models // present even when empty, so its choices are still checked
	}
	return models, checked
}

// checkFindings adds one account's adapter Check findings, marking them
// fromCheck, and reports whether any is an error.
func checkFindings(f *findings, account string, fs []adapter.Finding) (hasErr bool) {
	for _, x := range fs {
		subject := x.Account
		if subject == "" {
			subject = account
		}
		sev := string(x.Severity)
		f.add(sev, subject, x.Message)
		if f.fromCheck == nil {
			f.fromCheck = map[findingKey]bool{}
		}
		f.fromCheck[findingKey{sev, subject, x.Message}] = true
		hasErr = hasErr || sev == sevError
	}
	return hasErr
}

// loginExpiryFinding reports an account whose login (the refresh token, which
// renewals never extend) has expired or expires within window. A zero expiry
// is unknown, not a finding; a zero window turns the check off. It is machine
// state like an adapter Check finding, so the editor's gate ignores it. The
// message holds the absolute UTC time, never a relative one, because the UI
// compares findings by subject and message.
func loginExpiryFinding(f *findings, account, vendor string, expires, now time.Time, window time.Duration) {
	if window <= 0 || expires.IsZero() {
		return
	}
	tool, ok := toolNames[vendor]
	if !ok {
		tool = vendor
	}
	at := expires.UTC().Format("2006-01-02 15:04Z")
	var sev, msg string
	switch {
	case !now.Before(expires):
		sev, msg = sevError, "login expired "+at+"; renewal fails until you log in again in "+tool+" with this account"
	case expires.Sub(now) <= window:
		sev, msg = sevWarning, "login expires "+at+"; log in again in "+tool+" with this account before then"
	default:
		return
	}
	f.add(sev, account, msg)
	if f.fromCheck == nil {
		f.fromCheck = map[findingKey]bool{}
	}
	f.fromCheck[findingKey{sev, account, msg}] = true
}

// groupCheckFindings replaces Check findings with the same severity and
// message on two or more accounts by one finding naming them all.
func groupCheckFindings(f *findings) {
	type cause struct{ sev, msg string }
	by := map[cause][]string{}
	for _, x := range f.list {
		if f.fromCheck[x.key()] {
			c := cause{x.Severity, x.Message}
			by[c] = append(by[c], x.Subject)
		}
	}
	out := f.list[:0:0]
	done := map[cause]bool{}
	for _, x := range f.list {
		c := cause{x.Severity, x.Message}
		if !f.fromCheck[x.key()] || len(by[c]) < 2 {
			out = append(out, x)
			continue
		}
		if done[c] {
			continue
		}
		done[c] = true
		names := append([]string(nil), by[c]...)
		sort.Strings(names)
		g := Finding{Severity: x.Severity, Subject: strings.Join(names, ", "), Message: x.Message, Accounts: names}
		f.fromCheck[g.key()] = true
		out = append(out, g)
	}
	f.list = out
}

// savedFindings compares each capped choice's cap count with the windows of
// the account's saved reading, and reports a last usage call that failed
// unless the account is rate-limited or its Check finding (checked) is the
// cause; no lock, no network.
func savedFindings(f *findings, cfg *core.Config, d Deps, accounts []string, checked map[string]bool) {
	store := NewStore(d, cfg)
	saved := map[string]*adapter.Reading{}
	for _, n := range accounts {
		s, err := store.Saved(cfg.Accounts[n])
		if err != nil {
			f.add(sevWarning, n, "cannot read stored usage: "+err.Error())
			continue
		}
		saved[n] = s.Reading
		if LastAttemptFailed(s) && !s.RetryUntil.After(d.Now()) && !checked[n] {
			f.add(sevWarning, n, "last usage reading failed: "+s.LastError)
		}
		if s.Reading != nil {
			for _, note := range s.Reading.Notes {
				f.add(sevWarning, n, note)
			}
		}
	}
	for _, p := range sortedProfiles(cfg) {
		labels := profileLabels(cfg.Profiles[p])
		for i, c := range cfg.Profiles[p] {
			rd := saved[c.Account]
			if !c.Capped || rd == nil {
				continue
			}
			ordered := core.OrderWindows(rd.Windows, c.Model)
			_, warns := core.EffectiveCaps(c.Account, c.Cap, ordered, cfg.Settings.DefaultCap)
			for _, w := range warns {
				f.add(sevWarning, fmt.Sprintf("profile %s, choice %s", p, labels[i]), w)
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
