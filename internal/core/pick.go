package core

import (
	"fmt"
	"math"
	"math/rand/v2"
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
	// TokenExpired and RetryUntil are the store's structured state, for
	// callers that report it (ls); Pick judges Reading and Err only.
	TokenExpired bool
	RetryUntil   time.Time // zero = not rate-limited
	// Renewable: the account's adapter can renew an expired login, so the
	// expiry does not stop a refresh.
	Renewable bool
}

// Env supplies credentials and usage to Pick.
type Env interface {
	Credential(account string) adapter.CredStatus
	Usage(account string) AccountState // lazy; implementations memoise per account
}

// Shuffle permutes n items through swap, as rand.Shuffle does.
type Shuffle = func(n int, swap func(i, j int))

// Pick walks the profile's positions in order (spec §7) and returns the first
// usable choice; at a load balancing group it lets the group's algorithm pick
// a usable member (load balancing spec §5). Each account's usage is read at
// most once, and only when a capped choice reaches that step. shuffle is the
// groups' random source; nil means rand.Shuffle.
func Pick(cfg *Config, profile string, now time.Time, env Env, shuffle Shuffle) Result {
	choices, ok := cfg.Profiles[profile]
	if !ok {
		return Failed(profile, fmt.Sprintf("unknown profile %q; configured: %s",
			profile, strings.Join(cfg.ProfileNames(), ", ")))
	}
	if shuffle == nil {
		shuffle = rand.Shuffle
	}
	p := picker{cfg: cfg, now: now, env: env, shuffle: shuffle, states: map[string]AccountState{}}
	p.res.Profile = profile
	groups, lbs := ChoiceGroups(choices)
	labels := Labels(groups)
	for n, pos := range Positions(groups, lbs) {
		if groups[pos.Start] == 0 {
			if p.try(choices[pos.Start], p.evaluate) {
				return p.picked(choices[pos.Start], nil)
			}
			continue
		}
		if i, ok := p.group(choices[pos.Start:pos.End], pos.LoadBalance); ok {
			lb := &LBPick{Algorithm: pos.LoadBalance, Choice: n + 1, Member: labels[pos.Start+i]}
			return p.picked(choices[pos.Start+i], lb)
		}
	}
	reason := "no choice available"
	p.res.Status = StatusFailed
	p.res.Reason = &reason
	return p.res
}

type picker struct {
	cfg     *Config
	now     time.Time
	env     Env
	shuffle Shuffle
	states  map[string]AccountState // accounts already read in this pick
	res     Result
}

// try applies check to c and records a skip when c is not usable.
func (p *picker) try(c Choice, check func(Choice) (string, bool)) bool {
	why, usable := check(c)
	if !usable {
		p.res.Skipped = append(p.res.Skipped, Skip{Account: c.Account, Model: c.Model, Why: why})
	}
	return usable
}

// group picks a usable member of a load balancing group (load balancing
// spec §5): the local checks run on every member in file order, so a
// credential error does not depend on the shuffle; then the algorithm walks
// the survivors, which it is given, reading usage only for the members it
// asks about. The members it skipped are recorded in file order, so the
// output does not depend on the shuffle either. It returns the picked
// member's index in members.
func (p *picker) group(members []Choice, algorithm string) (int, bool) {
	whys := make([]string, len(members)) // non-empty: skipped, and why
	var survivors []int
	var alive []Choice
	for i, c := range members {
		if why, ok := p.local(c); ok {
			survivors, alive = append(survivors, i), append(alive, c)
		} else {
			whys[i] = why
		}
	}
	usable := func(i int) bool {
		why, ok := p.usageAllows(alive[i])
		if !ok {
			whys[survivors[i]] = why
		}
		return ok
	}
	i, ok := Algorithms[algorithm].Pick(alive, usable, p.shuffle)
	for j, why := range whys {
		if why != "" {
			p.res.Skipped = append(p.res.Skipped, Skip{Account: members[j].Account, Model: members[j].Model, Why: why})
		}
	}
	if !ok {
		return 0, false
	}
	return survivors[i], true
}

// evaluate applies steps 1–7 of spec §7 to c. It returns usable, or why not.
func (p *picker) evaluate(c Choice) (why string, usable bool) {
	if why, ok := p.local(c); !ok {
		return why, false
	}
	return p.usageAllows(c)
}

// local applies the steps that read no usage (spec §7 steps 1–2): the
// schedules and the credential source.
func (p *picker) local(c Choice) (why string, usable bool) {
	for _, name := range c.SkipDuring {
		if p.cfg.Schedules[name].Matches(p.now) {
			return "in schedule " + name, false
		}
	}
	if len(c.AllowDuring) > 0 && !p.matchesAny(c.AllowDuring) {
		noun := "schedule "
		if len(c.AllowDuring) > 1 {
			noun = "schedules "
		}
		return "outside " + noun + strings.Join(c.AllowDuring, ", "), false
	}
	// An expired credential is still usable: the usage path renews it, and
	// the TUI refreshes it on launch.
	if cred := p.env.Credential(c.Account); cred.Err != nil || !cred.Present {
		msg := cred.Source + ": not found"
		if cred.Err != nil {
			msg = cred.Err.Error()
		}
		p.addError(c.Account, msg)
		return "credential missing (see errors)", false
	}
	return "", true
}

// usageAllows applies the cap steps (spec §7 steps 3–7), reading the
// account's usage only when c is capped.
func (p *picker) usageAllows(c Choice) (why string, usable bool) {
	if !c.Capped {
		return "", true
	}
	st := p.usage(c.Account)
	if st.Reading == nil {
		return "usage unknown (see errors)", false
	}
	ordered := OrderWindows(ApplyResets(*st.Reading, p.now).Windows, c.Model)
	if len(ordered) == 0 {
		// No window applies to this model: usage is unknown, never 0%.
		p.addError(c.Account, "usage response had no windows")
		return "usage unknown (no windows reported)", false
	}
	caps, warnings := EffectiveCaps(c.Account, c.Cap, ordered, p.cfg.Settings.DefaultCap)
	p.addWarnings(warnings)
	for i, w := range ordered {
		if why, blocked := p.windowBlocks(c.Account, w, caps[i]); blocked {
			return why, false
		}
	}
	return "", true
}

func (p *picker) matchesAny(names []string) bool {
	for _, name := range names {
		if p.cfg.Schedules[name].Matches(p.now) {
			return true
		}
	}
	return false
}

// windowBlocks judges one window against its effective cap element. A pace
// element whose window lacks a length or reset time falls back to default_cap
// with a warning.
func (p *picker) windowBlocks(account string, w adapter.Window, cap CapElem) (string, bool) {
	label := FormatLength(w.Length)
	if cap.Kind == CapPace {
		if allowed, elapsed, ok := PaceAllowed(w, cap.N, p.now); ok {
			if w.UsedPct < allowed {
				return "", false
			}
			return fmt.Sprintf("%s window %s%% >= pace %s%% (%s, +%s)", label, formatPct(w.UsedPct),
				formatPct(math.Round(allowed*10)/10), paceWhere(w.Length, elapsed), formatPct(cap.N)), true
		}
		d := p.cfg.Settings.DefaultCap
		p.addWarnings([]string{fmt.Sprintf("%s: %s window has no reset time; pace cap used default_cap %s",
			account, label, formatPct(d))})
		cap = CapElem{CapFixed, d}
	}
	if w.UsedPct >= cap.N {
		return fmt.Sprintf("%s window %s%% >= cap %s", label, formatPct(w.UsedPct), formatPct(cap.N)), true
	}
	return "", false
}

// usage reads an account once per pick and records its error and warnings.
func (p *picker) usage(account string) AccountState {
	if st, ok := p.states[account]; ok {
		return st
	}
	st := p.env.Usage(account)
	p.states[account] = st
	switch {
	case st.Err != nil:
		p.addError(account, st.Err.Error())
	case st.Reading == nil:
		// Keeps "usage unknown (see errors)" from pointing at nothing.
		p.addError(account, "usage unavailable")
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

// picked is the result for c; lb is nil unless c is a group member.
func (p *picker) picked(c Choice, lb *LBPick) Result {
	acct := p.cfg.Accounts[c.Account]
	r := p.res
	r.Status = StatusOK
	if len(r.Errors) > 0 {
		r.Status = StatusOKWithErrors
	}
	r.Vendor = &acct.Vendor
	r.Account = &acct.Name
	r.Model = &c.Model
	if c.Thinking != nil {
		thinking := *c.Thinking
		r.Thinking = &thinking
	}
	r.AccountRef = &acct
	r.LoadBalance = lb
	return r
}

func formatPct(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
