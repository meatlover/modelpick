package live

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/app/apptest"
	"github.com/meatlover/modelpick/internal/web"
)

func rowOf(t *testing.T, rows []web.UsageRow, name string) web.UsageRow {
	t.Helper()
	for _, r := range rows {
		if r.Account == name {
			return r
		}
	}
	t.Fatalf("no row for %s", name)
	return web.UsageRow{}
}

// The page's reads never call a vendor (spec §8.3 Usage).
func (f *fixture) usageCalls() int {
	return f.claude.TotalUsage() + f.codex.TotalUsage() + f.goSrv.count()
}

func TestUsageRowFromSavedReading(t *testing.T) {
	f := newFixture(t)
	l := f.live()
	r := rowOf(t, l.Usage(), "claude-main")
	want := `{"account":"claude-main","vendor":"claude","state":"ok","reading_age_seconds":600,"can_refresh":true,"windows":[` +
		`{"label":"weekly","length":"7d","used_pct":58,"resets_at":"2026-10-24T00:00:00Z","scope":null},` +
		`{"label":"session","length":"5h","used_pct":0,"resets_at":"2026-10-22T11:00:00Z","scope":null},` +
		`{"label":"weekly-fable","length":"7d","used_pct":12,"resets_at":"","scope":"fable"},` +
		`{"label":"other","length":"7d","used_pct":1,"resets_at":"","scope":"claude-haiku-5-5"}]}`
	if got := mustJSON(t, r); got != want {
		t.Errorf("row:\n got %s\nwant %s", got, want)
	}
	l.State()
	l.Doctor()
	if n := f.usageCalls(); n != 0 {
		t.Errorf("reads made %d usage calls", n)
	}
}

// Ruling 2: the state vocabulary and can_refresh, from the saved state and
// the local credential only.
func TestUsageStates(t *testing.T) {
	cases := []struct {
		name, stored string
		expired      bool
		state        string
		canRefresh   bool
	}{
		{"no file", "", false, "no reading yet", true},
		{"expired, no reading", "", true, "expired token", false},
		{"expired with a reading", claudeStored, true, "expired token", false},
		{"rate-limited", `{"retry_until":"2026-10-22T13:00:00Z","last_error":"usage endpoint returned HTTP 429"}`,
			false, "rate-limited until 2026-10-22T13:00:00Z", false},
		{"retry passed", `{"retry_until":"2026-10-22T11:00:00Z"}`, false, "no reading yet", true},
		{"failed, no reading", `{"last_attempt":"2026-10-22T11:59:00Z","last_error":"usage endpoint returned HTTP 500"}`,
			false, "error: usage endpoint returned HTTP 500", true},
		{"failed after a reading", `{"last_good":{"taken_at":"2026-10-22T11:00:00Z","windows":[]},` +
			`"last_attempt":"2026-10-22T11:59:00Z","last_error":"usage endpoint returned HTTP 500"}`,
			false, "error: usage endpoint returned HTTP 500", true},
		{"failure before the reading", `{"last_good":{"taken_at":"2026-10-22T11:59:00Z","windows":[]},` +
			`"last_attempt":"2026-10-22T11:59:00Z","last_error":"stale"}`, false, "ok", true},
	}
	for _, c := range cases {
		f := newFixture(t)
		if c.stored != "" {
			apptest.WriteStored(t, f.d, "claude-rec", c.stored)
		}
		f.claude.Expired["claude-rec"] = c.expired
		r := rowOf(t, f.live().Usage(), "claude-rec")
		if r.State != c.state || r.CanRefresh != c.canRefresh {
			t.Errorf("%s: state %q can_refresh %v, want %q %v", c.name, r.State, r.CanRefresh, c.state, c.canRefresh)
		}
		if (r.ReadingAgeSeconds != nil) != strings.Contains(c.stored, "last_good") {
			t.Errorf("%s: reading_age_seconds %v", c.name, r.ReadingAgeSeconds)
		}
	}
}

func TestRefresh(t *testing.T) {
	f := newFixture(t)
	l := f.live()
	f.claude.Readings["claude-rec"] = apptest.ClaudeReading(5, 6)
	row, err := l.Refresh("claude-rec")
	if err != nil || row.State != "ok" || row.ReadingAgeSeconds == nil || len(row.Windows) != 2 {
		t.Fatalf("refresh claude-rec: %+v %v", row, err)
	}
	// Ruling 5: the next State shows the new reading's window labels.
	if got := mustJSON(t, l.State().Windows["claude-rec"]); got != `{"claude-sonnet-5-5":["session","weekly"]}` {
		t.Errorf("windows after refresh: %s", got)
	}
	l.Refresh("claude-rec")
	if f.claude.Calls["claude-rec"] != 2 {
		t.Errorf("Refresh bypasses the floor: %d calls, want 2", f.claude.Calls["claude-rec"])
	}
	if row, err := l.Refresh("go-1"); err != nil || len(row.Windows) != 3 || f.goSrv.count() != 1 {
		t.Errorf("refresh go-1: %+v %v (%d calls)", row, err, f.goSrv.count())
	}
}

func TestRefreshRefusalsAndFailures(t *testing.T) {
	f := newFixture(t)
	l := f.live()
	f.codex.Expired["codex-main"] = true
	cases := map[string]string{"codex-main": "codex-main: expired token", "nope": "nope: unknown account"}
	for name, want := range cases {
		_, err := l.Refresh(name)
		if r, ok := err.(web.Refused); !ok || r.Reason != want {
			t.Errorf("%s: %#v, want Refused %q", name, err, want)
		}
	}
	if f.codex.Calls["codex-main"] != 0 {
		t.Error("an expired token was refreshed")
	}
	f.claude.Errs["claude-main"] = errBoom
	_, err := l.Refresh("claude-main")
	var r web.Refused
	if err == nil || errors.As(err, &r) || !errors.Is(err, errBoom) {
		t.Errorf("failed call: %#v, want a plain error wrapping the cause", err)
	}
}

func (f *fixture) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.firsts...)
}

func waitFirst(t *testing.T, f *fixture) string {
	t.Helper()
	select {
	case n := <-f.firstCh:
		return n
	case <-time.After(5 * time.Second):
		t.Fatal("no first reading started")
		return ""
	}
}

// Ruling 3: one background first reading per account without a saved
// reading, at New and after account.add; never for one with a reading.
func TestFirstReadingOncePerAccount(t *testing.T) {
	f := newFixture(t)
	l := f.live()
	got := map[string]bool{}
	for i := 0; i < 3; i++ {
		got[waitFirst(t, f)] = true
	}
	if len(got) != 3 || !got["claude-rec"] || !got["codex-main"] || !got["go-1"] {
		t.Fatalf("first readings at New: %v", got)
	}
	s := l.State()
	args := json.RawMessage(`{"name":"claude-new","vendor":"claude","fields":{"config_dir":"~/.new"}}`)
	if _, err := l.Apply(web.OpRequest{Base: s.SHA256, Op: "account.add", Args: args}); err != nil {
		t.Fatal(err)
	}
	if n := waitFirst(t, f); n != "claude-new" {
		t.Fatalf("after account.add: %s", n)
	}
	l.maybeFirst("claude-new")
	l.maybeFirst("claude-rec")
	l.maybeFirst("claude-main")
	l.Usage()
	l.State()
	if r := f.recorded(); len(r) != 4 {
		t.Errorf("first readings %v, want 4 (each account once, none for claude-main)", r)
	}
}

// The default first reading goes through the store without refresh: the
// floor and an expired token hold.
func TestReadFirstHonoursTheStore(t *testing.T) {
	f := newFixture(t)
	l := f.live()
	for i := 0; i < 3; i++ {
		waitFirst(t, f)
	}
	f.claude.Readings["claude-rec"] = apptest.ClaudeReading(5, 6)
	f.codex.Expired["codex-main"] = true
	l.readFirst("claude-rec")
	l.readFirst("claude-rec")
	l.readFirst("codex-main")
	if f.claude.Calls["claude-rec"] != 1 || f.codex.Calls["codex-main"] != 0 {
		t.Errorf("calls: claude-rec %d (want 1), codex-main %d (want 0)",
			f.claude.Calls["claude-rec"], f.codex.Calls["codex-main"])
	}
	if r := rowOf(t, l.Usage(), "claude-rec"); r.State != "ok" || r.ReadingAgeSeconds == nil {
		t.Errorf("after the first reading: %+v", r)
	}
}

// New with no trigger takes the readings itself, in the background.
func TestNewTakesFirstReadings(t *testing.T) {
	f := newFixture(t)
	apptest.WriteStored(t, f.d, "claude-rec", claudeStored)
	apptest.WriteStored(t, f.d, "codex-main", claudeStored)
	New(f.d, f.path)
	cfg, err := app.LoadConfig(f.d)
	if err != nil {
		t.Fatal(err)
	}
	store := app.NewStore(f.d, cfg)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s, err := store.Saved(cfg.Accounts["go-1"]); err == nil && s.Reading != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("go-1 has no reading after New")
}

// Ruling 4: each doctor run re-runs Check; models are listed once.
func TestDoctorRerunsChecksKeepsModels(t *testing.T) {
	f := newFixture(t)
	l := f.live()
	l.State()
	d1 := l.Doctor()
	l.Doctor()
	if f.claude.CheckCalls["claude-main"] != 2 || f.claude.ModelCalls["claude-main"] != 1 {
		t.Errorf("Check %d (want 2), Models %d (want 1)", f.claude.CheckCalls["claude-main"], f.claude.ModelCalls["claude-main"])
	}
	if d1.Errors+d1.Warnings != len(d1.Findings) || d1.Findings == nil {
		t.Errorf("doctor %+v", d1)
	}
}
