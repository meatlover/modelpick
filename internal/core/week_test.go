package core

import (
	"reflect"
	"testing"
)

func weekOf(t *testing.T, rules ...DocRule) Week {
	t.Helper()
	w, err := RulesWeek(rules)
	if err != nil {
		t.Fatalf("RulesWeek(%v): %v", rules, err)
	}
	return w
}

func TestWeekRoundTrip(t *testing.T) {
	peak := DocRule{Days: []string{"mon", "tue", "wed", "thu", "fri"}, UTC: []string{"01:00-04:00", "06:00-10:00"}}
	weekend := []DocRule{
		{Days: []string{"sat"}, UTC: []string{"08:00-12:00"}},
		{Days: []string{"sun"}, UTC: []string{"09:00-11:00"}},
	}
	for name, rules := range map[string][]DocRule{"deepseek-peak": {peak}, "weekend-mornings": weekend} {
		w := weekOf(t, rules...)
		out := WeekRules(w)
		if name == "deepseek-peak" && !reflect.DeepEqual(out, []DocRule{peak}) {
			t.Errorf("WeekRules(peak) = %+v, want one rule mon..fri", out)
		}
		if name == "weekend-mornings" && !reflect.DeepEqual(out, weekend) {
			t.Errorf("WeekRules(weekend) = %+v, want %+v", out, weekend)
		}
		if again := weekOf(t, out...); again != w {
			t.Errorf("%s: round trip changed the week", name)
		}
	}
}

func TestEmptyWeek(t *testing.T) {
	w := weekOf(t, DocRule{Days: []string{}, UTC: []string{}})
	if w != (Week{}) {
		t.Error("empty rule must give the empty week")
	}
	if got := WeekRules(w); got != nil {
		t.Errorf("WeekRules(empty) = %+v, want nil", got)
	}
}

func TestRulesWeekRefusals(t *testing.T) {
	bad := []DocRule{
		{Days: []string{"mon"}, UTC: nil},
		{Days: nil, UTC: []string{"01:00-02:00"}},
		{Days: []string{"mon"}, UTC: []string{"24:00-01:00"}},
		{Days: []string{"xyz"}, UTC: []string{"01:00-02:00"}},
	}
	for _, r := range bad {
		if _, err := RulesWeek([]DocRule{r}); err == nil {
			t.Errorf("%+v: expected error", r)
		}
	}
}

func TestCrossMidnightCells(t *testing.T) {
	c := weekOf(t, DocRule{Days: []string{"sat"}, UTC: []string{"23:00-01:00"}}).Cells()
	if c[6][23] != 1 || c[0][0] != 1 {
		t.Errorf("Sat 23 = %v, Sun 00 = %v, want 1 and 1", c[6][23], c[0][0])
	}
	if c[0][1] != 0 || c[6][22] != 0 {
		t.Error("neighbouring hours must be 0")
	}
}

func TestPartialCells(t *testing.T) {
	c := weekOf(t, DocRule{Days: []string{"mon"}, UTC: []string{"01:30-02:00"}}).Cells()
	if c[1][1] != 0.5 || c[1][2] != 0 {
		t.Errorf("Mon 01 = %v, Mon 02 = %v, want 0.5 and 0", c[1][1], c[1][2])
	}
}

func TestPaintKeepsOtherMinutes(t *testing.T) {
	w := weekOf(t, DocRule{Days: []string{"mon"}, UTC: []string{"01:30-02:00"}})
	w.Paint([][2]int{{1, 2}}, true)
	want := []DocRule{{Days: []string{"mon"}, UTC: []string{"01:30-03:00"}}}
	if got := WeekRules(w); !reflect.DeepEqual(got, want) {
		t.Errorf("after paint on: %+v, want %+v", got, want)
	}
	w.Paint([][2]int{{1, 1}}, false)
	want = []DocRule{{Days: []string{"mon"}, UTC: []string{"02:00-03:00"}}}
	if got := WeekRules(w); !reflect.DeepEqual(got, want) {
		t.Errorf("after paint off: %+v, want %+v", got, want)
	}
	w.Paint([][2]int{{9, 1}, {1, 30}, {-1, 0}}, true) // out of range: ignored
	if got := WeekRules(w); !reflect.DeepEqual(got, want) {
		t.Errorf("out-of-range paint changed the week: %+v", got)
	}
}

func TestWeekRulesSundayWrap(t *testing.T) {
	var w Week
	w.Paint([][2]int{{6, 23}, {0, 0}}, true)
	want := []DocRule{
		{Days: []string{"sat"}, UTC: []string{"23:00-24:00"}},
		{Days: []string{"sun"}, UTC: []string{"00:00-01:00"}},
	}
	if got := WeekRules(w); !reflect.DeepEqual(got, want) {
		t.Errorf("WeekRules = %+v, want %+v", got, want)
	}
}

func TestFullDayAndFullWeekRoundTrip(t *testing.T) {
	var day, all Week
	for h := 0; h < 24; h++ {
		day.Paint([][2]int{{1, h}}, true)
		for d := 0; d < 7; d++ {
			all.Paint([][2]int{{d, h}}, true)
		}
	}
	if got := WeekRules(day); !reflect.DeepEqual(got, []DocRule{{Days: []string{"mon"}, UTC: []string{"00:00-24:00"}}}) {
		t.Errorf("full day = %+v", got)
	}
	for name, w := range map[string]Week{"day": day, "week": all} {
		back, err := RulesWeek(WeekRules(w))
		if err != nil || back != w {
			t.Errorf("%s: round trip err=%v equal=%v", name, err, back == w)
		}
	}
}

func TestScheduleWeekMatchesRulesWeek(t *testing.T) {
	rules := []DocRule{{Days: []string{"mon", "sun"}, UTC: []string{"22:00-02:00", "05:00-06:00"}}}
	s, err := ParseSchedule(rules[0].Days, rules[0].UTC)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := s.Week(), weekOf(t, rules...); got != want {
		t.Errorf("Schedule.Week differs from RulesWeek")
	}
	if (Schedule{}).Week() != (Week{}) {
		t.Errorf("no rules must give an empty week")
	}
}
