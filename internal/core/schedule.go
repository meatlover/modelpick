// Package core holds the vendor-neutral decision logic of modelpick.
package core

import (
	"fmt"
	"strings"
	"time"
)

// MinuteRange is a span of minutes since 00:00 UTC. End < Start means the
// range crosses midnight. Start is inclusive, End exclusive.
type MinuteRange struct{ Start, End int }

// ScheduleRule is a set of UTC weekdays and time ranges. A range with
// Start < End may end at 1440 ("24:00").
type ScheduleRule struct {
	Days   map[time.Weekday]bool
	Ranges []MinuteRange
}

// Schedule matches when any of its rules matches; no rules match never.
type Schedule struct{ Rules []ScheduleRule }

var dayNames = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

// ParseSchedule builds a one-rule Schedule from day names (mon..sun) and
// "HH:MM-HH:MM" UTC ranges. Both lists empty is the empty rule, which matches
// nothing. See parseRange for the range rules.
func ParseSchedule(days, utc []string) (Schedule, error) {
	rule := ScheduleRule{Days: map[time.Weekday]bool{}}
	for _, d := range days {
		wd, ok := dayNames[d]
		if !ok {
			return Schedule{}, fmt.Errorf("unknown day %q (want mon..sun)", d)
		}
		rule.Days[wd] = true
	}
	for _, r := range utc {
		mr, err := parseRange(r)
		if err != nil {
			return Schedule{}, err
		}
		rule.Ranges = append(rule.Ranges, mr)
	}
	return Schedule{Rules: []ScheduleRule{rule}}, nil
}

// parseRange parses "HH:MM-HH:MM". "24:00" is allowed only as the end; the
// range must be longer than 0 minutes and shorter than 24 hours, except 00:00-24:00 (the whole day); an end
// earlier than the start crosses midnight.
func parseRange(r string) (MinuteRange, error) {
	startText, endText, ok := strings.Cut(r, "-")
	if !ok {
		return MinuteRange{}, fmt.Errorf("range %q: want HH:MM-HH:MM", r)
	}
	start, err := parseClock(startText)
	if err != nil {
		return MinuteRange{}, fmt.Errorf("range %q: %w", r, err)
	}
	end := minutesPerDay
	if endText != "24:00" {
		if end, err = parseClock(endText); err != nil {
			return MinuteRange{}, fmt.Errorf("range %q: %w", r, err)
		}
	}
	if start == end {
		return MinuteRange{}, fmt.Errorf("range %q: start equals end", r)
	}
	return MinuteRange{Start: start, End: end}, nil
}

const minutesPerDay = 1440

// parseClock parses a zero-padded "HH:MM" into minutes since midnight.
func parseClock(text string) (int, error) {
	t, err := time.Parse("15:04", text)
	if err != nil || len(text) != 5 {
		return 0, fmt.Errorf("invalid time %q (want HH:MM, 00:00-23:59)", text)
	}
	return t.Hour()*60 + t.Minute(), nil
}

// Matches reports whether now, evaluated in UTC, falls inside the schedule.
// A range that crosses midnight belongs to the day it starts on.
func (s Schedule) Matches(now time.Time) bool {
	for _, r := range s.Rules {
		if r.matches(now) {
			return true
		}
	}
	return false
}

func (s ScheduleRule) matches(now time.Time) bool {
	now = now.UTC()
	m := now.Hour()*60 + now.Minute()
	today := s.Days[now.Weekday()]
	yesterday := s.Days[(now.Weekday()+6)%7]
	for _, r := range s.Ranges {
		if r.Start < r.End {
			if today && m >= r.Start && m < r.End {
				return true
			}
			continue
		}
		if today && m >= r.Start {
			return true
		}
		if yesterday && m < r.End {
			return true
		}
	}
	return false
}
