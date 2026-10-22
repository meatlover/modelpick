package core

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
)

// AccountState is what an Env knows about an account's usage for this pick.
type AccountState struct {
	Reading  *adapter.Reading // nil = usage unknown
	Err      error            // a failure in this run
	Warnings []string
}

// Env supplies credentials and usage to Pick.
type Env interface {
	Credential(account string) adapter.CredStatus
	Usage(account string) AccountState // lazy; implementations memoise per account
}

// Pick walks the profile's choices in order (spec §7) and returns the first
// usable one. Each account's usage is read at most once, and only when a
// capped choice reaches that step.
func Pick(cfg *Config, profile string, now time.Time, env Env) Result {
	choices, ok := cfg.Profiles[profile]
	if !ok {
		return Failed(profile, fmt.Sprintf("unknown profile %q; configured: %s",
			profile, strings.Join(cfg.ProfileNames(), ", ")))
	}
	p := picker{cfg: cfg, now: now, env: env, states: map[string]AccountState{}}
	p.res.Profile = profile
	for _, c := range choices {
		why, usable := p.evaluate(c)
		if !usable {
			p.res.Skipped = append(p.res.Skipped, Skip{Account: c.Account, Model: c.Model, Why: why})
			continue
		}
		return p.picked(c)
	}
	reason := "no choice available"
	p.res.Status = StatusFailed
	p.res.Reason = &reason
	return p.res
}

type picker struct {
	cfg    *Config
	now    time.Time
	env    Env
	states map[string]AccountState // accounts already read in this pick
	res    Result
}

// evaluate applies steps 1–7 of spec §7 to c. It returns usable, or why not.
func (p *picker) evaluate(c Choice) (why string, usable bool) {
	for _, name := range c.SkipDuring {
		if p.cfg.Schedules[name].Matches(p.now) {
			return "in schedule " + name, false
		}
	}
	// An expired credential is still usable: the TUI refreshes it on launch.
	if cred := p.env.Credential(c.Account); cred.Err != nil || !cred.Present {
		msg := cred.Source + ": not found"
		if cred.Err != nil {
			msg = cred.Err.Error()
		}
		p.addError(c.Account, msg)
		return "credential missing (see errors)", false
	}
	if !c.Capped {
		return "", true
	}
	st := p.usage(c.Account)
	if st.Reading == nil {
		return "usage unknown (see errors)", false
	}
	ordered := OrderWindows(ApplyResets(*st.Reading, p.now).Windows, c.Model)
	caps, warnings := EffectiveCaps(c.Account, c.Cap, ordered, p.cfg.Settings.DefaultCap)
	p.addWarnings(warnings)
	for i, w := range ordered {
		if w.UsedPct >= caps[i] {
			return fmt.Sprintf("%s window %s%% >= cap %s",
				FormatLength(w.Length), formatPct(w.UsedPct), formatPct(caps[i])), false
		}
	}
	return "", true
}

// usage reads an account once per pick and records its error and warnings.
func (p *picker) usage(account string) AccountState {
	if st, ok := p.states[account]; ok {
		return st
	}
	st := p.env.Usage(account)
	p.states[account] = st
	if st.Err != nil {
		p.addError(account, st.Err.Error())
	}
	p.addWarnings(st.Warnings)
	return st
}

func (p *picker) addError(account, msg string) {
	e := RunError{Account: account, Error: msg}
	if !slices.Contains(p.res.Errors, e) {
		p.res.Errors = append(p.res.Errors, e)
	}
}

func (p *picker) addWarnings(ws []string) {
	for _, w := range ws {
		if !slices.Contains(p.res.Warnings, w) {
			p.res.Warnings = append(p.res.Warnings, w)
		}
	}
}

func (p *picker) picked(c Choice) Result {
	acct := p.cfg.Accounts[c.Account]
	r := p.res
	r.Status = StatusOK
	if len(r.Errors) > 0 {
		r.Status = StatusOKWithErrors
	}
	r.Vendor = &acct.Vendor
	r.Account = &acct.Name
	r.Model = &c.Model
	r.Thinking = c.Thinking
	r.AccountRef = &acct
	return r
}

func formatPct(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
