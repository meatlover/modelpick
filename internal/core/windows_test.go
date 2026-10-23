package core

import (
	"reflect"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
)

func labels(ws []adapter.Window) []string {
	var out []string
	for _, w := range ws {
		out = append(out, w.Label)
	}
	return out
}

func claudeWindows() []adapter.Window {
	return []adapter.Window{
		{Label: "weekly", Length: 7 * 24 * time.Hour},
		{Label: "fable", Length: 7 * 24 * time.Hour, ScopeModelName: "Fable"},
		{Label: "session", Length: 5 * time.Hour},
	}
}

func TestOrderWindowsClaudeFable(t *testing.T) {
	got := labels(OrderWindows(claudeWindows(), "claude-fable-5-1"))
	if want := []string{"session", "weekly", "fable"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("fable: got %v want %v", got, want)
	}
	got = labels(OrderWindows(claudeWindows(), "claude-opus-5-5"))
	if want := []string{"session", "weekly"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("opus: got %v want %v", got, want)
	}
}

func TestOrderWindowsUnknownLast(t *testing.T) {
	ws := []adapter.Window{
		{Label: "mystery1"},
		{Label: "fable", Length: 7 * 24 * time.Hour, ScopeModelName: "Fable"},
		{Label: "session", Length: 5 * time.Hour},
		{Label: "mystery2"},
	}
	got := labels(OrderWindows(ws, "claude-fable-5-1"))
	if want := []string{"session", "fable", "mystery1", "mystery2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestScopeMatches(t *testing.T) {
	cases := []struct {
		w     adapter.Window
		model string
		want  bool
	}{
		{adapter.Window{}, "anything", true},
		{adapter.Window{ScopeModelID: "m1"}, "m1", true},
		{adapter.Window{ScopeModelID: "m1", ScopeModelName: "Fable"}, "claude-fable-5-1", false},
		{adapter.Window{ScopeModelName: "fable"}, "claude-fable-5-1", true},
		{adapter.Window{ScopeModelName: "Fable"}, "claude-opus-5-5", false},
		{adapter.Window{ScopeModelName: "Fab"}, "claude-fable-5-1", false},
	}
	for i, c := range cases {
		if got := ScopeMatches(c.w, c.model); got != c.want {
			t.Errorf("case %d: got %v want %v", i, got, c.want)
		}
	}
}

func twoWindows() []adapter.Window {
	return []adapter.Window{
		{Label: "a", Length: 5 * time.Hour},
		{Label: "b", Length: 7 * 24 * time.Hour},
	}
}

func TestEffectiveCapsFill(t *testing.T) {
	caps, warns := EffectiveCaps("codex-main", []CapElem{{CapFixed, 98}}, twoWindows(), 95)
	if want := []CapElem{{CapFixed, 98}, {CapFixed, 95}}; !reflect.DeepEqual(caps, want) {
		t.Fatalf("caps %v", caps)
	}
	want := []string{"codex-main: account reports 2 windows (5h, 7d), cap has 1 value; filled position 2 with default_cap 95"}
	if !reflect.DeepEqual(warns, want) {
		t.Fatalf("warns %q", warns)
	}
}

func TestEffectiveCapsEmptyArray(t *testing.T) {
	caps, warns := EffectiveCaps("x", []CapElem{}, twoWindows(), 95)
	if want := []CapElem{{CapFixed, 95}, {CapFixed, 95}}; !reflect.DeepEqual(caps, want) {
		t.Fatalf("caps %v", caps)
	}
	want := []string{"x: account reports 2 windows (5h, 7d), cap has 0 values; filled positions 1, 2 with default_cap 95"}
	if !reflect.DeepEqual(warns, want) {
		t.Fatalf("warns %q", warns)
	}
}

func TestEffectiveCapsTrim(t *testing.T) {
	ws := []adapter.Window{{Label: "a", Length: 7 * 24 * time.Hour}}
	caps, warns := EffectiveCaps("codex-main", []CapElem{{CapFixed, 90}, {CapFixed, 80}, {CapFixed, 70}}, ws, 95)
	if want := []CapElem{{CapFixed, 90}}; !reflect.DeepEqual(caps, want) {
		t.Fatalf("caps %v", caps)
	}
	want := []string{"codex-main: account reports 1 window (7d), cap has 3 values; ignored the last 2"}
	if !reflect.DeepEqual(warns, want) {
		t.Fatalf("warns %q", warns)
	}
}

func TestEffectiveCapsExactNoWarning(t *testing.T) {
	caps, warns := EffectiveCaps("x", []CapElem{{CapFixed, 90}, {CapFixed, 80}}, twoWindows(), 95)
	if !reflect.DeepEqual(caps, []CapElem{{CapFixed, 90}, {CapFixed, 80}}) || len(warns) != 0 {
		t.Fatalf("caps %v warns %q", caps, warns)
	}
}

func TestEffectiveCapsUnknownWindow(t *testing.T) {
	ws := append(twoWindows(), adapter.Window{Label: "weird"})
	caps, warns := EffectiveCaps("claude-main", []CapElem{{CapFixed, 90}, {CapFixed, 80}}, ws, 95)
	if want := []CapElem{{CapFixed, 90}, {CapFixed, 80}, {CapFixed, 95}}; !reflect.DeepEqual(caps, want) {
		t.Fatalf("caps %v", caps)
	}
	want := []string{`claude-main: unrecognised window "weird" capped at default_cap 95`}
	if !reflect.DeepEqual(warns, want) {
		t.Fatalf("warns %q", warns)
	}
}

func TestApplyResetsZeroesPassedWindow(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	r := adapter.Reading{Windows: []adapter.Window{
		{Label: "old", UsedPct: 80, ResetsAt: now.Add(-time.Minute)},
		{Label: "edge", UsedPct: 70, ResetsAt: now},
		{Label: "future", UsedPct: 60, ResetsAt: now.Add(time.Hour)},
	}}
	out := ApplyResets(r, now)
	if out.Windows[0].UsedPct != 0 || out.Windows[1].UsedPct != 0 || out.Windows[2].UsedPct != 60 {
		t.Fatalf("got %+v", out.Windows)
	}
	if r.Windows[0].UsedPct != 80 {
		t.Fatal("input mutated")
	}
}

func TestApplyResetsKeepsWindowWithoutReset(t *testing.T) {
	r := adapter.Reading{Windows: []adapter.Window{{Label: "x", UsedPct: 42}}}
	out := ApplyResets(r, time.Now())
	if out.Windows[0].UsedPct != 42 {
		t.Fatalf("got %+v", out.Windows)
	}
}

func TestFormatLength(t *testing.T) {
	cases := map[time.Duration]string{
		5 * time.Hour: "5h", 7 * 24 * time.Hour: "7d", 30 * 24 * time.Hour: "30d",
		90 * time.Minute: "90m", 36 * time.Hour: "36h", 0: "?",
	}
	for d, want := range cases {
		if got := FormatLength(d); got != want {
			t.Errorf("%v: got %q want %q", d, got, want)
		}
	}
}

func TestEffectiveCapsDefaultElem(t *testing.T) {
	caps, warns := EffectiveCaps("x", []CapElem{{CapDefault, 0}, {CapFixed, 80}}, twoWindows(), 95)
	if want := []CapElem{{CapFixed, 95}, {CapFixed, 80}}; !reflect.DeepEqual(caps, want) {
		t.Fatalf("caps %v", caps)
	}
	if len(warns) != 0 {
		t.Fatalf("warns %q", warns)
	}
}
