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

// report collects doctor findings, dropping exact repeats (same severity and
// message) so a problem shared by several accounts prints once.
type report struct {
	findings []adapter.Finding
	seen     map[string]bool
}

func (r *report) add(sev adapter.Severity, subject, msg string) {
	key := string(sev) + "\x00" + msg
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

	// Each account: adapter Check (binary, credential, expiry, key line),
	// once; then its model list for the thinking checks.
	e := newEnv(d, cfg, false)
	models := map[string][]adapter.Model{}
	modelsOK := map[string]bool{}
	for _, n := range accounts {
		acct, a, err := e.adapterFor(n)
		if err != nil {
			r.add(adapter.SeverityError, n, err.Error())
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		for _, f := range a.Check(ctx, acct) {
			subject := f.Account
			if subject == "" {
				subject = n
			}
			r.add(f.Severity, subject, f.Message)
		}
		ms, err := a.Models(ctx, acct)
		cancel()
		if err != nil {
			r.add(adapter.SeverityWarning, n, "cannot list models, thinking values not checked: "+usageError(acct.Vendor, err).Error())
			continue
		}
		models[n], modelsOK[n] = ms, true
	}

	for _, p := range profiles {
		for i, c := range cfg.Profiles[p] {
			if modelsOK[c.Account] {
				doctorThinking(r, p, i, c, models[c.Account])
			}
		}
	}

	// Cap count vs the last stored reading; no lock, no network.
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
		for _, c := range cfg.Profiles[p] {
			rd := peeked[c.Account]
			if !c.Capped || rd == nil {
				continue
			}
			ordered := core.OrderWindows(rd.Windows, c.Model)
			_, warns := core.EffectiveCaps(c.Account, c.Cap, ordered, cfg.Settings.DefaultCap)
			for _, w := range warns {
				r.add(adapter.SeverityWarning, c.Account, w)
			}
		}
	}

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
