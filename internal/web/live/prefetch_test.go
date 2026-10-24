package live

import (
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/app/apptest"
)

const slowConfig = `{"version": 1, "accounts": {
  "c1": {"vendor": "claude", "config_dir": "~/.c1"},
  "c2": {"vendor": "claude", "config_dir": "~/.c2"},
  "c3": {"vendor": "claude", "config_dir": "~/.c3"}}, "profiles": {}}`

func slowFixture(t *testing.T) (*fixture, *apptest.Fake) {
	t.Helper()
	fk := apptest.NewFake("claude")
	fk.ModelDelay = 200 * time.Millisecond
	for _, n := range []string{"c1", "c2", "c3"} {
		fk.ModelList[n] = []adapter.Model{{ID: "m-" + n}}
	}
	d := apptest.NewDeps(t, slowConfig, fk)
	return &fixture{t: t, d: d, path: d.Getenv("MODELPICK_CONFIG"), claude: fk, firstCh: make(chan string, 16)}, fk
}

func totalModelCalls(fk *apptest.Fake) int {
	n := 0
	for _, a := range []string{"c1", "c2", "c3"} {
		n += fk.ModelCallCount(a)
	}
	return n
}

func TestColdStateListsAccountsConcurrently(t *testing.T) {
	f, fk := slowFixture(t)
	l := f.live()
	start := time.Now()
	s := l.State()
	took := time.Since(start)
	if took > 550*time.Millisecond { // sequential is 600ms; 550 leaves margin for a loaded CI box without admitting it
		t.Errorf("cold State took %v with three 200ms accounts; want < 550ms", took)
	}
	for _, n := range []string{"c1", "c2", "c3"} {
		if len(s.Models[n]) != 1 {
			t.Errorf("models of %s: %v", n, s.Models[n])
		}
	}
	if got := totalModelCalls(fk); got != 3 {
		t.Errorf("Models calls %d, want 3", got)
	}
}

func TestColdDoctorChecksAccountsConcurrently(t *testing.T) {
	f, _ := slowFixture(t)
	l := f.live()
	start := time.Now()
	l.Doctor()
	if took := time.Since(start); took > 450*time.Millisecond {
		t.Errorf("cold Doctor took %v; want < 450ms", took)
	}
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func totalDone(fk *apptest.Fake) int {
	n := 0
	for _, a := range []string{"c1", "c2", "c3"} {
		n += fk.ModelDoneCount(a)
	}
	return n
}

func TestNewPrefetchesModelLists(t *testing.T) {
	f, fk := slowFixture(t)
	l := New(f.d, f.path)
	defer l.Close()
	waitFor(t, "three finished listings", func() bool { return totalDone(fk) == 3 })
	start := time.Now()
	l.State()
	if after := totalModelCalls(fk); after != 3 {
		t.Errorf("Models calls after State %d; want 3, unchanged by State", after)
	}
	if took := time.Since(start); took > 100*time.Millisecond {
		t.Errorf("State after prefetch took %v", took)
	}
}

func TestClosePrefetchWaits(t *testing.T) {
	f, fk := slowFixture(t)
	fk.ModelDelay = 5 * time.Second
	l := New(f.d, f.path)
	waitFor(t, "three listings started", func() bool { return totalModelCalls(fk) == 3 })
	l.Close()
	// The fake unwinds 100ms after cancel: only a Close that waits sees all
	// three listings returned.
	ended := 0
	for _, a := range []string{"c1", "c2", "c3"} {
		ended += fk.ModelEndedCount(a)
	}
	if ended != 3 {
		t.Errorf("%d of 3 listings had returned when Close returned", ended)
	}
	if totalDone(fk) != 0 {
		t.Errorf("a listing ran to the end instead of being cancelled")
	}
}

func TestPanickingAdapterDoesNotCrashState(t *testing.T) {
	f, fk := slowFixture(t)
	fk.ModelDelay = 0
	fk.ModelPanic = map[string]bool{"c2": true}
	l := New(f.d, f.path)
	defer l.Close()
	s := l.State()
	if len(s.Models["c1"]) != 1 || len(s.Models["c2"]) != 0 || len(s.Models["c3"]) != 1 {
		t.Errorf("models %v", s.Models)
	}
	l.Doctor()
}
