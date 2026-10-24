package app

import (
	"fmt"

	"github.com/meatlover/modelpick/internal/core"
)

// opScheduleAdd creates an empty schedule (written {"days": [], "utc": []}).
func opScheduleAdd(doc *core.Doc, a opArgs, _ OpEnv) error {
	_, taken := doc.Schedules[a.Name]
	if err := checkNew("schedule", a.Name, taken); err != nil {
		return err
	}
	if doc.Schedules == nil {
		doc.Schedules = map[string][]core.DocRule{}
	}
	doc.Schedules[a.Name] = nil
	return nil
}

// scheduleUses counts the choices that list the schedule in skip or allow.
func scheduleUses(doc *core.Doc, name string) int {
	n := 0
	for _, cs := range doc.Profiles {
		for _, c := range cs {
			if contains(c.SkipDuring, name) || contains(c.AllowDuring, name) {
				n++
			}
		}
	}
	return n
}

// without drops s from list; an emptied list becomes nil (not written).
func without(list []string, s string) []string {
	var out []string
	for _, x := range list {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

// opScheduleRemove refuses a schedule in use unless forced; forced, it takes
// the name out of every choice's lists first.
func opScheduleRemove(doc *core.Doc, a opArgs, _ OpEnv) error {
	if _, ok := doc.Schedules[a.Name]; !ok {
		return refuse("Unknown schedule %s", a.Name)
	}
	if n := scheduleUses(doc, a.Name); n > 0 && !a.Force {
		return &Refused{Reason: fmt.Sprintf("Schedule %s is used by %s", a.Name, plural(n, "choice")), Uses: n}
	}
	for _, cs := range doc.Profiles {
		for j := range cs {
			cs[j].SkipDuring = without(cs[j].SkipDuring, a.Name)
			cs[j].AllowDuring = without(cs[j].AllowDuring, a.Name)
		}
	}
	delete(doc.Schedules, a.Name)
	return nil
}

// opSchedulePaint sets whole UTC hours on or off and rewrites the schedule
// in canonical form; minutes outside the painted hours are kept.
func opSchedulePaint(doc *core.Doc, a opArgs, _ OpEnv) error {
	rules, ok := doc.Schedules[a.Name]
	if !ok {
		return refuse("Unknown schedule %s", a.Name)
	}
	for _, c := range a.Cells {
		if c[0] < 0 || c[0] > 6 || c[1] < 0 || c[1] > 23 {
			return refuse("Cell [%d, %d] is outside day 0–6, hour 0–23", c[0], c[1])
		}
	}
	w, err := core.RulesWeek(rules)
	if err != nil {
		return refuse("Schedule %s: %v", a.Name, err)
	}
	w.Paint(a.Cells, a.On)
	doc.Schedules[a.Name] = core.WeekRules(w)
	return nil
}
