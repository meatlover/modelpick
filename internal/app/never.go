package app

import (
	"fmt"
	"sort"

	"github.com/meatlover/modelpick/internal/core"
)

// scheduleFindings warns about each schedule that matches no minute.
func scheduleFindings(cfg *core.Config, add func(sev, subject, msg string)) {
	for name, s := range cfg.Schedules {
		if s.Week() == (core.Week{}) {
			add(sevWarning, "schedule "+name, "empty")
		}
	}
}

// neverFindings warns about each choice that an earlier choice always beats
// (spec §8.4): the later one never gets picked. Only the lowest such j is named.
func neverFindings(cfg *core.Config, profile string, add func(sev, subject, msg string)) {
	choices := cfg.Profiles[profile]
	runs := make([]core.Week, len(choices))
	for i, c := range choices {
		runs[i] = runTime(cfg, c)
	}
	for i := 1; i < len(choices); i++ {
		for j := 0; j < i; j++ {
			if !subset(runs[i], runs[j]) || !outranks(cfg, choices[j], choices[i]) {
				continue
			}
			add(sevWarning, fmt.Sprintf("profile %s, choice %d", profile, i+1),
				fmt.Sprintf("never picked, choice %d always wins first", j+1))
			break
		}
	}
}

// runTime is the minutes of the UTC week a choice may run: every minute but
// its skip_during union, or its allow_during union.
func runTime(cfg *core.Config, c core.Choice) core.Week {
	var w core.Week
	if len(c.AllowDuring) > 0 {
		for _, name := range c.AllowDuring {
			w = union(w, cfg.Schedules[name].Week())
		}
		return w
	}
	var skipped core.Week
	for _, name := range c.SkipDuring {
		skipped = union(skipped, cfg.Schedules[name].Week())
	}
	for d := range w {
		for m := range w[d] {
			w[d][m] = !skipped[d][m]
		}
	}
	return w
}

func union(a, b core.Week) core.Week {
	for d := range a {
		for m := range a[d] {
			a[d][m] = a[d][m] || b[d][m]
		}
	}
	return a
}

// subset reports whether every minute of a is in b.
func subset(a, b core.Week) bool {
	for d := range a {
		for m := range a[d] {
			if a[d][m] && !b[d][m] {
				return false
			}
		}
	}
	return true
}

// outranks reports whether j passes its caps whenever i does: j is uncapped,
// or both are the same account and model with j's limit at least i's at every
// position. A fixed/pace mix at any position is not compared.
func outranks(cfg *core.Config, j, i core.Choice) bool {
	if !j.Capped {
		return true
	}
	if !i.Capped || j.Account != i.Account || j.Model != i.Model {
		return false
	}
	n := max(len(j.Cap), len(i.Cap))
	for p := 0; p < n; p++ {
		a, b := capAt(j.Cap, p, cfg.Settings.DefaultCap), capAt(i.Cap, p, cfg.Settings.DefaultCap)
		if a.Kind != b.Kind || a.N < b.N {
			return false
		}
	}
	return true
}

// capAt is position p of a cap array with "default" and absent positions
// read as the default cap.
func capAt(caps []core.CapElem, p int, defaultCap float64) core.CapElem {
	if p >= len(caps) || caps[p].Kind == core.CapDefault {
		return core.CapElem{Kind: core.CapFixed, N: defaultCap}
	}
	return caps[p]
}

func sortedProfiles(cfg *core.Config) []string {
	out := make([]string, 0, len(cfg.Profiles))
	for n := range cfg.Profiles {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
