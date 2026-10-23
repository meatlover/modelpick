package core

import (
	"testing"
	"time"
)

func mustParse(t *testing.T, days, utc []string) Schedule {
	t.Helper()
	s, err := ParseSchedule(days, utc)
	if err != nil {
		t.Fatalf("ParseSchedule(%v, %v): %v", days, utc, err)
	}
	return s
}

func at(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// 2026-10-22 is a Thursday; 10-23 Friday; 10-24 Saturday.
func TestScheduleMatchesPeak(t *testing.T) {
	peak := mustParse(t, []string{"mon", "tue", "wed", "thu", "fri"}, []string{"01:00-04:00", "06:00-10:00"})
	tokyo := time.FixedZone("JST", 9*3600)
	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"start inclusive", at(t, "2026-10-22T01:00:00Z"), true},
		{"last second", at(t, "2026-10-22T03:59:59Z"), true},
		{"end exclusive", at(t, "2026-10-22T04:00:00Z"), false},
		{"second range", at(t, "2026-10-22T07:30:00Z"), true},
		{"saturday", at(t, "2026-10-24T07:30:00Z"), false},
		{"non-UTC zone", at(t, "2026-10-22T07:30:00Z").In(tokyo), true},
		{"non-UTC zone saturday UTC but friday local", at(t, "2026-10-24T07:30:00Z").In(time.FixedZone("X", -12*3600)), false},
	}
	for _, c := range cases {
		if got := peak.Matches(c.now); got != c.want {
			t.Errorf("%s: Matches(%v) = %v, want %v", c.name, c.now, got, c.want)
		}
	}
}

func TestScheduleCrossesMidnight(t *testing.T) {
	s := mustParse(t, []string{"fri"}, []string{"22:00-02:00"})
	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"fri 23:00", at(t, "2026-10-23T23:00:00Z"), true},
		{"sat 01:00 belongs to fri", at(t, "2026-10-24T01:00:00Z"), true},
		{"sat 02:00 end exclusive", at(t, "2026-10-24T02:00:00Z"), false},
		{"fri 01:00 belongs to thu", at(t, "2026-10-23T01:00:00Z"), false},
		{"sat 23:00 not fri", at(t, "2026-10-24T23:00:00Z"), false},
	}
	for _, c := range cases {
		if got := s.Matches(c.now); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestParseScheduleErrors(t *testing.T) {
	cases := []struct {
		name      string
		days, utc []string
	}{
		{"hour out of range", []string{"mon"}, []string{"25:00-26:00"}},
		{"start equals end", []string{"mon"}, []string{"01:00-01:00"}},
		{"bad day", []string{"funday"}, []string{"01:00-02:00"}},
		{"no dash", []string{"mon"}, []string{"01:00"}},
		{"minute out of range", []string{"mon"}, []string{"01:60-02:00"}},
		{"unpadded", []string{"mon"}, []string{"1:00-2:00"}},
	}
	for _, c := range cases {
		if _, err := ParseSchedule(c.days, c.utc); err == nil {
			t.Errorf("%s: expected error", c.name)
		}
	}
}

func TestParseScheduleTwentyFourEnd(t *testing.T) {
	s := mustParse(t, []string{"mon"}, []string{"22:00-24:00"})
	if !s.Matches(at(t, "2026-10-19T23:59:00Z")) || s.Matches(at(t, "2026-10-20T00:00:00Z")) {
		t.Error("22:00-24:00 on mon must cover mon 23:59 and stop at tue 00:00")
	}
	for _, bad := range []string{"24:00-01:00", "00:00-24:00", "24:00-24:00", "01:00-24:01"} {
		if _, err := ParseSchedule([]string{"mon"}, []string{bad}); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

func TestScheduleEmptyMatchesNothing(t *testing.T) {
	for _, s := range []Schedule{{}, mustParse(t, nil, nil)} {
		if s.Matches(at(t, "2026-10-22T07:30:00Z")) {
			t.Errorf("%+v matched", s)
		}
	}
}

func TestScheduleAnyRuleMatches(t *testing.T) {
	r1 := mustParse(t, []string{"sat"}, []string{"08:00-12:00"}).Rules[0]
	r2 := mustParse(t, []string{"sun"}, []string{"09:00-11:00"}).Rules[0]
	s := Schedule{Rules: []ScheduleRule{r1, r2}}
	if !s.Matches(at(t, "2026-10-25T10:00:00Z")) || !s.Matches(at(t, "2026-10-24T10:00:00Z")) {
		t.Error("sun 10:00 and sat 10:00 must both match")
	}
	if s.Matches(at(t, "2026-10-25T08:00:00Z")) {
		t.Error("sun 08:00 must not match")
	}
}
