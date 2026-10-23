package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/core"
)

// report collects doctor findings, dropping exact repeats (same subject,
// severity and message). Messages need not name their account, so the
// subject is part of the key.
type report struct {
	findings []adapter.Finding
	seen     map[string]bool
}

func (r *report) add(sev adapter.Severity, subject, msg string) {
	key := string(sev) + "\x00" + subject + "\x00" + msg
	if r.seen[key] {
		return
	}
	if r.seen == nil {
		r.seen = map[string]bool{}
	}
	r.seen[key] = true
	r.findings = append(r.findings, adapter.Finding{Severity: sev, Account: subject, Message: msg})
}

// runDoctor is `modelpick doctor`: validates the config and everything it
// depends on. It never calls Usage and never takes the store lock (Peek only).
func runDoctor(args []string, stdout, stderr io.Writer, d Deps) int {
	if len(args) > 0 {
		return badUsage(stderr, fmt.Sprintf("doctor: unexpected argument %q", args[0]))
	}
	var r report
	cfg, err := loadConfig(d)
	if err != nil {
		r.add(adapter.SeverityError, "config", err.Error())
	} else {
		doctorChecks(&r, cfg, d)
	}
	return writeReport(stdout, &r)
}

func writeReport(w io.Writer, r *report) int {
	errs, warns := 0, 0
	for _, f := range r.findings {
		label := "WARN  "
		if f.Severity == adapter.SeverityError {
			label = "ERROR "
			errs++
		} else {
			warns++
		}
		fmt.Fprintf(w, "%s %s: %s\n", label, f.Account, f.Message)
	}
	if len(r.findings) == 0 {
		fmt.Fprintln(w, "no problems found")
		return 0
	}
	fmt.Fprintf(w, "%d error(s), %d warning(s)\n", errs, warns)
	if errs > 0 {
		return 1
	}
	return 0
}

func doctorChecks(r *report, cfg *core.Config, d Deps) {
	accounts := make([]string, 0, len(cfg.Accounts))
	for n := range cfg.Accounts {
		accounts = append(accounts, n)
	}
	sort.Strings(accounts)
	profiles := make([]string, 0, len(cfg.Profiles))
	for n := range cfg.Profiles {
		profiles = append(profiles, n)
	}
	sort.Strings(profiles)

	models := doctorAccounts(r, cfg, d, accounts)
	for _, p := range profiles {
		for i, c := range cfg.Profiles[p] {
			if ms, ok := models[c.Account]; ok {
				doctorThinking(r, p, i, c, ms)
			}
		}
	}
	doctorCaps(r, cfg, d, accounts, profiles)
	doctorUncapped(r, cfg, profiles)
}

// doctorAccounts runs each account's adapter Check (binary, credential,
// expiry, key line) once, and returns the model list of every account
// whose models could be listed, for the thinking checks.
func doctorAccounts(r *report, cfg *core.Config, d Deps, accounts []string) map[string][]adapter.Model {
	e := newEnv(d, cfg, false)
	models := map[string][]adapter.Model{}
	for _, n := range accounts {
		acct, a, err := e.adapterFor(n)
		if err != nil {
			r.add(adapter.SeverityError, n, err.Error())
			continue
		}
		checkCtx, cancelCheck := context.WithTimeout(context.Background(), 10*time.Second)
		findings := a.Check(checkCtx, acct)
		cancelCheck()
		for _, f := range findings {
			subject := f.Account
			if subject == "" {
				subject = n
			}
			r.add(f.Severity, subject, f.Message)
		}
		modelsCtx, cancelModels := context.WithTimeout(context.Background(), 10*time.Second)
		ms, err := a.Models(modelsCtx, acct)
		cancelModels()
		if err != nil {
			r.add(adapter.SeverityWarning, n, "cannot list models, thinking values not checked: "+usageError(acct.Vendor, err).Error())
			continue
		}
		models[n] = ms // present even when empty, so its choices are still checked
	}
	return models
}

// doctorCaps compares each capped choice's cap count with the windows of
// the account's last stored reading; no lock, no network.
func doctorCaps(r *report, cfg *core.Config, d Deps, accounts, profiles []string) {
	store := newStore(d, cfg)
	peeked := map[string]*adapter.Reading{}
	for _, n := range accounts {
		rd, err := store.Peek(cfg.Accounts[n])
		if err != nil {
			r.add(adapter.SeverityWarning, n, "cannot read stored usage: "+err.Error())
			continue
		}
		peeked[n] = rd
		if rd != nil {
			for _, note := range rd.Notes {
				r.add(adapter.SeverityWarning, n, note)
			}
		}
	}
	for _, p := range profiles {
		for i, c := range cfg.Profiles[p] {
			rd := peeked[c.Account]
			if !c.Capped || rd == nil {
				continue
			}
			ordered := core.OrderWindows(rd.Windows, c.Model)
			_, warns := core.EffectiveCaps(c.Account, c.Cap, ordered, cfg.Settings.DefaultCap)
			for _, w := range warns {
				r.add(adapter.SeverityWarning, fmt.Sprintf("profile %s[%d]", p, i), w)
			}
		}
	}
}

// doctorUncapped warns about a profile with no uncapped choice.
func doctorUncapped(r *report, cfg *core.Config, profiles []string) {
	for _, p := range profiles {
		uncapped := false
		for _, c := range cfg.Profiles[p] {
			uncapped = uncapped || !c.Capped
		}
		if !uncapped {
			r.add(adapter.SeverityWarning, "profile "+p, fmt.Sprintf("profile %s has no uncapped choice; pick can return failed", p))
		}
	}
}

// doctorThinking checks one choice's model and thinking value against the
// account's model list. A null thinking is always fine.
func doctorThinking(r *report, profile string, i int, c core.Choice, ms []adapter.Model) {
	where := fmt.Sprintf("profiles.%s[%d]", profile, i)
	subject := "profile " + profile
	var m *adapter.Model
	for j := range ms {
		if ms[j].ID == c.Model {
			m = &ms[j]
			break
		}
	}
	if m == nil {
		r.add(adapter.SeverityWarning, subject, fmt.Sprintf("%s: model %s not offered by %s", where, c.Model, c.Account))
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
	r.add(adapter.SeverityError, subject, fmt.Sprintf("%s: thinking %q not supported by %s (supported: %s)", where, *c.Thinking, c.Model, supported))
}
