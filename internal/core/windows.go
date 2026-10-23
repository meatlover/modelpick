package core

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
)

// ApplyResets returns a copy of r in which every window whose known reset time
// has passed reads 0% used. The input is never mutated.
func ApplyResets(r adapter.Reading, now time.Time) adapter.Reading {
	out := r
	out.Windows = make([]adapter.Window, len(r.Windows))
	copy(out.Windows, r.Windows)
	for i := range out.Windows {
		w := &out.Windows[i]
		if !w.ResetsAt.IsZero() && !w.ResetsAt.After(now) {
			w.UsedPct = 0
		}
	}
	return out
}

// ScopeMatches reports whether w applies to model: general windows always do;
// a model-id scope must equal model; a model-name scope must equal one
// "-"-separated token of model, case-insensitively.
func ScopeMatches(w adapter.Window, model string) bool {
	if w.ScopeModelID == "" && w.ScopeModelName == "" {
		return true
	}
	if w.ScopeModelID != "" {
		return w.ScopeModelID == model
	}
	for _, tok := range strings.Split(model, "-") {
		if strings.EqualFold(tok, w.ScopeModelName) {
			return true
		}
	}
	return false
}

func isGeneral(w adapter.Window) bool { return w.ScopeModelID == "" && w.ScopeModelName == "" }

// OrderWindows orders the windows that apply to model: general windows of
// known length ascending, then windows scoped to model ascending, then
// unknown-length windows in input order. Windows scoped to another model are
// dropped.
func OrderWindows(ws []adapter.Window, model string) []adapter.Window {
	var general, scoped, unknown []adapter.Window
	for _, w := range ws {
		if !ScopeMatches(w, model) {
			continue
		}
		switch {
		case w.Length <= 0:
			unknown = append(unknown, w)
		case isGeneral(w):
			general = append(general, w)
		default:
			scoped = append(scoped, w)
		}
	}
	byLength := func(s []adapter.Window) {
		sort.SliceStable(s, func(i, j int) bool { return s[i].Length < s[j].Length })
	}
	byLength(general)
	byLength(scoped)
	out := make([]adapter.Window, 0, len(general)+len(scoped)+len(unknown))
	out = append(out, general...)
	out = append(out, scoped...)
	return append(out, unknown...)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// EffectiveCaps returns one cap per ordered window. Known-length windows take
// cap[i] when present ("default" reads as defaultCap), else defaultCap; unknown-length windows always take
// defaultCap. Mismatches and unknown windows are described in warnings.
func EffectiveCaps(account string, cap []CapElem, ordered []adapter.Window, defaultCap float64) (caps []CapElem, warnings []string) {
	var knownLabels []string
	for _, w := range ordered {
		if w.Length > 0 {
			knownLabels = append(knownLabels, FormatLength(w.Length))
		}
	}
	n, m := len(knownLabels), len(cap)
	head := fmt.Sprintf("%s: account reports %d %s (%s), cap has %d %s",
		account, n, plural(n, "window", "windows"), strings.Join(knownLabels, ", "),
		m, plural(m, "value", "values"))
	if m < n {
		var pos []string
		for i := m + 1; i <= n; i++ {
			pos = append(pos, fmt.Sprint(i))
		}
		warnings = append(warnings, fmt.Sprintf("%s; filled %s %s with default_cap %g",
			head, plural(len(pos), "position", "positions"), strings.Join(pos, ", "), defaultCap))
	} else if m > n {
		warnings = append(warnings, fmt.Sprintf("%s; ignored the last %d", head, m-n))
	}

	caps = make([]CapElem, len(ordered))
	for i, w := range ordered {
		if w.Length > 0 && i < m {
			caps[i] = cap[i]
			if cap[i].Kind == CapDefault {
				caps[i] = CapElem{CapFixed, defaultCap}
			}
			continue
		}
		caps[i] = CapElem{CapFixed, defaultCap}
		if w.Length <= 0 {
			warnings = append(warnings, fmt.Sprintf("%s: unrecognised window %q capped at default_cap %g",
				account, w.Label, defaultCap))
		}
	}
	return caps, warnings
}

// FormatLength renders a window length as 5h, 7d, 90m; 0 is "?".
func FormatLength(d time.Duration) string {
	switch {
	case d <= 0:
		return "?"
	case d%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	default:
		return fmt.Sprintf("%dm", int(d.Round(time.Minute)/time.Minute))
	}
}
