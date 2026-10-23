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
	if took > 450*time.Millisecond {
		t.Errorf("cold State took %v with three 200ms accounts; want < 450ms", took)
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

func TestNewPrefetchesModelLists(t *testing.T) {
	f, fk := slowFixture(t)
	l := New(f.d, f.path)
	defer l.Close()
	deadline := time.Now().Add(3 * time.Second)
	for totalModelCalls(fk) < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond) // let the slowest listing finish and cache
	before := totalModelCalls(fk)
	start := time.Now()
	l.State()
	if after := totalModelCalls(fk); after != before || before != 3 {
		t.Errorf("Models calls before State %d, after %d; want 3 and unchanged", before, after)
	}
	if took := time.Since(start); took > 100*time.Millisecond {
		t.Errorf("State after prefetch took %v", took)
	}
}

func TestClosePrefetchWaits(t *testing.T) {
	f, fk := slowFixture(t)
	fk.ModelDelay = 400 * time.Millisecond
	l := New(f.d, f.path)
	time.Sleep(50 * time.Millisecond)
	l.Close()
	n := totalModelCalls(fk)
	time.Sleep(600 * time.Millisecond)
	if after := totalModelCalls(fk); after != n {
		t.Errorf("Models calls grew from %d to %d after Close", n, after)
	}
	if n != 3 {
		t.Errorf("prefetch had started %d listings, want 3", n)
	}
}
