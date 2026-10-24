package core

import (
	"fmt"
	"math"
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
	// Fewer values than windows is not a warning: the rest use default_cap,
	// which is what the page shows (default boxes).
	if m > n {
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
				account, DisplayLabel(w), defaultCap))
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

// PaceAllowed is the share of window w a pace position permits at now:
// elapsed% + n capped at 100 (spec §3.2). elapsed is the share of the window
// already gone, 0–100. ok is false when the length or reset time is unknown.
func PaceAllowed(w adapter.Window, n float64, now time.Time) (allowed, elapsed float64, ok bool) {
	if w.Length <= 0 || w.ResetsAt.IsZero() {
		return 0, 0, false
	}
	start := w.ResetsAt.Add(-w.Length)
	elapsed = float64(now.Sub(start)) / float64(w.Length) * 100
	elapsed = math.Max(0, math.Min(100, elapsed))
	return math.Min(100, elapsed+n), elapsed, true
}

// paceWhere says how far into the window the pace line stands: "day 2.7 of 7"
// for whole-day lengths, else "hour 2.5 of 5".
func paceWhere(length time.Duration, elapsed float64) string {
	unit, name := time.Hour, "hour"
	if length%(24*time.Hour) == 0 {
		unit, name = 24*time.Hour, "day"
	}
	pos := elapsed / 100 * float64(length) / float64(unit)
	return fmt.Sprintf("%s %s of %s", name, formatPct(math.Round(pos*10)/10),
		formatPct(math.Round(float64(length)/float64(unit)*10)/10))
}

// DisplayLabel is the name a window carries on screen: its length, plus the
// model it is scoped to ("5h", "7d", "7d Fable"). A window of unknown length
// keeps the vendor's label.
func DisplayLabel(w adapter.Window) string {
	if w.Length <= 0 {
		return w.Label
	}
	label := FormatLength(w.Length)
	scope := w.ScopeModelName
	if scope == "" {
		scope = w.ScopeModelID
	}
	if scope != "" {
		label += " " + scope
	}
	return label
}

// DisplayLabels labels one reading's windows. Windows whose display labels
// collide keep their vendor labels: unambiguous beats pretty.
func DisplayLabels(ws []adapter.Window) []string {
	out := make([]string, len(ws))
	seen := map[string]int{}
	for i, w := range ws {
		out[i] = DisplayLabel(w)
		seen[out[i]]++
	}
	for i, w := range ws {
		if seen[out[i]] > 1 {
			out[i] = w.Label
		}
	}
	return out
}
