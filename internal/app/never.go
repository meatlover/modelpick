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

// profileLabels names each flat choice of a profile by its position ("1",
// "2a", "2b", …): the form doctor subjects use.
func profileLabels(choices []core.Choice) []string {
	groups := make([]int, len(choices))
	for i, c := range choices {
		groups[i] = c.Group
	}
	return core.Labels(groups)
}

// profilePositions folds a profile's choices into positions.
func profilePositions(choices []core.Choice) []core.Position {
	groups, lbs := make([]int, len(choices)), make([]string, len(choices))
	for i, c := range choices {
		groups[i], lbs[i] = c.Group, c.LoadBalance
	}
	return core.Positions(groups, lbs)
}

// neverFindings warns about each position that an earlier position always
// beats (spec §8.4, load balancing §7): the later one never gets picked. A
// plain choice is covered by an earlier position when the members of that
// position that outrank it can run at every minute it can; a group is covered
// when every member is. The warning names the lowest covering position (for a
// group, the highest of its members' lowest ones), once. Members of one group
// never warn against each other; identicalMemberFindings covers duplicates.
func neverFindings(cfg *core.Config, profile string, add func(sev, subject, msg string)) {
	choices := cfg.Profiles[profile]
	runs := make([]core.Week, len(choices))
	for i, c := range choices {
		runs[i] = runTime(cfg, c)
	}
	positions := profilePositions(choices)
	for q := 1; q < len(positions); q++ {
		by := -1
		for i := positions[q].Start; i < positions[q].End; i++ {
			p := coveringPosition(cfg, choices, runs, positions[:q], i)
			if p < 0 {
				by = -1
				break
			}
			by = max(by, p)
		}
		if by >= 0 {
			add(sevWarning, fmt.Sprintf("profile %s, choice %d", profile, q+1),
				fmt.Sprintf("never picked, choice %d always wins first", by+1))
		}
	}
}

// coveringPosition is the lowest of the earlier positions that has at least
// one member outranking choice i and whose outranking members together run at
// every minute choice i can, or -1. A position with no outranking member
// covers nothing, even a choice that never runs.
func coveringPosition(cfg *core.Config, choices []core.Choice, runs []core.Week, earlier []core.Position, i int) int {
	for p, pos := range earlier {
		var covered core.Week
		outranked := false
		for j := pos.Start; j < pos.End; j++ {
			if outranks(cfg, choices[j], choices[i]) {
				covered, outranked = union(covered, runs[j]), true
			}
		}
		if outranked && subset(runs[i], covered) {
			return p
		}
	}
	return -1
}

// identicalMemberFindings warns about a group member with the same account,
// model and thinking as an earlier member of its group: it doubles that
// model's chance, which is likely a slip.
func identicalMemberFindings(profile string, choices []core.Choice, add func(sev, subject, msg string)) {
	labels := profileLabels(choices)
	for _, pos := range profilePositions(choices) {
		if pos.LoadBalance == "" {
			continue
		}
		for k := pos.Start + 1; k < pos.End; k++ {
			for j := pos.Start; j < k; j++ {
				if sameModelChoice(choices[j], choices[k]) {
					add(sevWarning, fmt.Sprintf("profile %s, choice %s", profile, labels[k]),
						fmt.Sprintf("same as choice %s; doubles its chance in the load balancing group", labels[j]))
					break
				}
			}
		}
	}
}

func sameModelChoice(a, b core.Choice) bool {
	if a.Account != b.Account || a.Model != b.Model || (a.Thinking == nil) != (b.Thinking == nil) {
		return false
	}
	return a.Thinking == nil || *a.Thinking == *b.Thinking
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
