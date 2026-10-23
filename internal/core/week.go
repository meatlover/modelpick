package core

import (
	"fmt"
	"strings"
	"time"
)

// Week is the UTC minutes of a week a schedule covers; day 0 is Sunday.
type Week [7][1440]bool

// DocRule is one `{days, utc}` object of a schedule as written in the file.
type DocRule struct {
	Days []string `json:"days"`
	UTC  []string `json:"utc"`
}

// weekOrder is the order the file lists days in: mon..sun.
var weekOrder = [7]time.Weekday{
	time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday,
}

// RulesWeek paints the rules onto a Week. A rule with both lists empty adds
// nothing; one with exactly one list empty is refused.
func RulesWeek(rules []DocRule) (Week, error) {
	var w Week
	for i, r := range rules {
		if len(r.Days) == 0 && len(r.UTC) == 0 {
			continue
		}
		if len(r.Days) == 0 || len(r.UTC) == 0 {
			return Week{}, fmt.Errorf("rule %d: days and utc must not be empty", i+1)
		}
		s, err := ParseSchedule(r.Days, r.UTC)
		if err != nil {
			return Week{}, fmt.Errorf("rule %d: %w", i+1, err)
		}
		w.paintRule(s.Rules[0])
	}
	return w, nil
}

func (w *Week) paintRule(r ScheduleRule) {
	for day := range r.Days {
		for _, mr := range r.Ranges {
			if mr.Start < mr.End {
				w.fill(int(day), mr.Start, mr.End, true)
				continue
			}
			w.fill(int(day), mr.Start, minutesPerDay, true)
			w.fill((int(day)+1)%7, 0, mr.End, true)
		}
	}
}

func (w *Week) fill(day, from, to int, on bool) {
	for m := from; m < to; m++ {
		w[day][m] = on
	}
}

// Paint sets whole hours: each cell is {UTC day (0 = Sunday), hour}. Cells
// outside the grid are ignored; other minutes are untouched.
func (w *Week) Paint(cells [][2]int, on bool) {
	for _, c := range cells {
		if c[0] < 0 || c[0] > 6 || c[1] < 0 || c[1] > 23 {
			continue
		}
		w.fill(c[0], c[1]*60, c[1]*60+60, on)
	}
}

// Cells returns the covered fraction of each hour.
func (w Week) Cells() [7][24]float64 {
	var out [7][24]float64
	for d := range w {
		var count [24]int
		for m, on := range w[d] {
			if on {
				count[m/60]++
			}
		}
		for h, n := range count {
			out[d][h] = float64(n) / 60
		}
	}
	return out
}

// WeekRules is the canonical file form: per UTC day, ranges that never cross
// midnight; days with identical ranges grouped; groups ordered by first day
// (mon..sun), days listed mon..sun. An empty week gives nil.
func WeekRules(w Week) []DocRule {
	var out []DocRule
	groupOf := map[string]int{}
	for _, day := range weekOrder {
		ranges := dayRanges(w[day])
		if len(ranges) == 0 {
			continue
		}
		key := strings.Join(ranges, ",")
		i, ok := groupOf[key]
		if !ok {
			i = len(out)
			groupOf[key] = i
			out = append(out, DocRule{UTC: ranges})
		}
		out[i].Days = append(out[i].Days, dayLabel(day))
	}
	return out
}

func dayLabel(d time.Weekday) string { return strings.ToLower(d.String()[:3]) }

// dayRanges merges one day's covered minutes into "HH:MM-HH:MM" strings.
func dayRanges(day [1440]bool) []string {
	var out []string
	for m := 0; m < minutesPerDay; m++ {
		if !day[m] {
			continue
		}
		start := m
		for m < minutesPerDay && day[m] {
			m++
		}
		out = append(out, clock(start)+"-"+clock(m))
	}
	return out
}

func clock(m int) string { return fmt.Sprintf("%02d:%02d", m/60, m%60) }
