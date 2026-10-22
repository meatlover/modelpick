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

// Schedule is a set of UTC weekdays and time ranges.
type Schedule struct {
	Days   map[time.Weekday]bool
	Ranges []MinuteRange
}

var dayNames = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

// ParseSchedule builds a Schedule from day names (mon..sun) and "HH:MM-HH:MM"
// UTC ranges. A range whose start equals its end is an error.
func ParseSchedule(days, utc []string) (Schedule, error) {
	s := Schedule{Days: map[time.Weekday]bool{}}
	for _, d := range days {
		wd, ok := dayNames[d]
		if !ok {
			return Schedule{}, fmt.Errorf("unknown day %q (want mon..sun)", d)
		}
		s.Days[wd] = true
	}
	for _, r := range utc {
		startText, endText, ok := strings.Cut(r, "-")
		if !ok {
			return Schedule{}, fmt.Errorf("range %q: want HH:MM-HH:MM", r)
		}
		start, err := parseClock(startText)
		if err != nil {
			return Schedule{}, fmt.Errorf("range %q: %w", r, err)
		}
		end, err := parseClock(endText)
		if err != nil {
			return Schedule{}, fmt.Errorf("range %q: %w", r, err)
		}
		if start == end {
			return Schedule{}, fmt.Errorf("range %q: start equals end", r)
		}
		s.Ranges = append(s.Ranges, MinuteRange{Start: start, End: end})
	}
	return s, nil
}

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
