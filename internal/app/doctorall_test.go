package app_test

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/app/apptest"
	"github.com/meatlover/modelpick/internal/core"
	"github.com/meatlover/modelpick/internal/usage"
)

// allEnv is the healthy docConfig (claude-main, codex-main) with a good
// reading for claude-main at 11:55Z, so only codex-main can be due a call.
type allEnv struct {
	t      *testing.T
	d      app.Deps
	cfg    *core.Config
	claude *apptest.Fake
	codex  *apptest.Fake
}

func newAllEnv(t *testing.T, codexStored string) *allEnv {
	t.Helper()
	claude, codex := apptest.DoctorFakes()
	d := apptest.NewDeps(t, docConfig("", codexA+`}`), claude, codex)
	apptest.WriteStored(t, d, "claude-main", `{`+reading1155+`}`)
	if codexStored != "" {
		apptest.WriteStored(t, d, "codex-main", codexStored)
	}
	cfg, err := app.LoadConfig(d)
	if err != nil {
		t.Fatal(err)
	}
	return &allEnv{t: t, d: d, cfg: cfg, claude: claude, codex: codex}
}

func (e *allEnv) run() []app.Finding {
	return app.DoctorAll(context.Background(), e.cfg, e.d, nil)
}

func (e *allEnv) saved(account string) usage.Saved {
	e.t.Helper()
	s, err := app.NewStore(e.d, e.cfg).Saved(e.cfg.Accounts[account])
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

func wantCalls(t *testing.T, e *allEnv, want int) {
	t.Helper()
	if got := e.codex.TotalUsage() + e.claude.TotalUsage(); got != want {
		t.Fatalf("usage calls = %d, want %d", got, want)
	}
}

func TestDoctorAllUsesFreshReading(t *testing.T) {
	e := newAllEnv(t, `{"last_good":{"taken_at":"2026-10-22T11:50:00Z","windows":[]}}`)
	e.run()
	wantCalls(t, e, 0)
}

func TestDoctorAllCallsWhenReadingOld(t *testing.T) {
	e := newAllEnv(t, `{`+reading1140+`}`)
	e.codex.Readings["codex-main"] = apptest.ClaudeReading(10, 20)
	e.run()
	wantCalls(t, e, 1)
	if r := e.saved("codex-main").Reading; r == nil || !r.TakenAt.Equal(apptest.Now) {
		t.Fatalf("stored reading = %+v, want one taken at %v", r, apptest.Now)
	}
}

func TestDoctorAllCallsWithoutReading(t *testing.T) {
	e := newAllEnv(t, "")
	e.codex.Readings["codex-main"] = apptest.ClaudeReading(10, 20)
	e.run()
	wantCalls(t, e, 1)
}

func TestDoctorAllIgnoresAttemptFloor(t *testing.T) {
	e := newAllEnv(t, `{"last_good":{"taken_at":"2026-10-22T11:30:00Z","windows":[]},`+
		`"last_attempt":"2026-10-22T11:58:00Z","last_error":"usage endpoint returned HTTP 503"}`)
	e.codex.Readings["codex-main"] = apptest.ClaudeReading(10, 20)
	e.run()
	wantCalls(t, e, 1)
}

func TestDoctorAllHonoursRetryAfter(t *testing.T) {
	e := newAllEnv(t, `{`+reading1100+`,"retry_until":"2026-10-22T12:10:00Z"}`)
	e.run()
	wantCalls(t, e, 0)
}

func TestDoctorAllSkipsExpiredToken(t *testing.T) {
	e := newAllEnv(t, `{`+reading1100+`}`)
	e.codex.Expired["codex-main"] = true
	e.run()
	wantCalls(t, e, 0)
}

func TestDoctorAllSkipsCheckError(t *testing.T) {
	e := newAllEnv(t, `{`+reading1100+`}`)
	e.codex.Checks["codex-main"] = []adapter.Finding{{Severity: adapter.SeverityError, Message: "claude not found on PATH"}}
	got := e.run()
	wantCalls(t, e, 0)
	want := []app.Finding{{Severity: "error", Subject: "codex-main", Message: "claude not found on PATH"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findings = %+v, want %+v", got, want)
	}
}

func TestDoctorAllReportsFailure(t *testing.T) {
	e := newAllEnv(t, `{`+reading1100+`}`)
	e.codex.Errs["codex-main"] = errors.New("dial tcp: connection refused")
	got := e.run()
	wantCalls(t, e, 1)
	want := []app.Finding{{Severity: "warning", Subject: "codex-main",
		Message: "last usage reading failed: dial tcp: connection refused"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findings = %+v, want %+v", got, want)
	}
}

// goAll is goConfig(3) with all three accounts due a call.
func goAll(t *testing.T) (*apptest.Fake, *core.Config, app.Deps) {
	t.Helper()
	fake := apptest.NewFake("opencode-go")
	d := apptest.NewDeps(t, goConfig(3), fake)
	cfg, err := app.LoadConfig(d)
	if err != nil {
		t.Fatal(err)
	}
	return fake, cfg, d
}

func TestDoctorAllConcurrent(t *testing.T) {
	fake, cfg, d := goAll(t)
	fake.UsageDelay = 200 * time.Millisecond
	start := time.Now()
	app.DoctorAll(context.Background(), cfg, d, nil)
	if took := time.Since(start); took >= 450*time.Millisecond {
		t.Fatalf("took %v, want < 450ms (three 200ms calls in parallel)", took)
	}
	if got := fake.TotalUsage(); got != 3 {
		t.Fatalf("usage calls = %d, want 3", got)
	}
}

func TestDoctorAllCancelled(t *testing.T) {
	fake, cfg, d := goAll(t)
	fake.UsageDelay = 5 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	app.DoctorAll(ctx, cfg, d, nil)
	if took := time.Since(start); took >= time.Second {
		t.Fatalf("took %v after cancel, want < 1s", took)
	}
	store := app.NewStore(d, cfg)
	for _, n := range []string{"go-1", "go-2", "go-3"} {
		s, err := store.Saved(cfg.Accounts[n])
		if err != nil || s.LastError != "" {
			t.Errorf("%s: saved %+v err %v, want no last_error", n, s, err)
		}
	}
}

func TestDoctorAllUnwritableCache(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write a read-only directory")
	}
	e := newAllEnv(t, "")
	dir := app.NewStore(e.d, e.cfg).Dir
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	e.codex.Readings["codex-main"] = apptest.ClaudeReading(10, 20)
	got := e.run()
	wantCalls(t, e, 0)
	var warns []app.Finding
	for _, f := range got {
		if strings.HasPrefix(f.Message, "cannot update stored usage: ") {
			warns = append(warns, f)
		}
	}
	if len(warns) != 1 || warns[0].Severity != "warning" || warns[0].Subject != "codex-main" ||
		!strings.Contains(warns[0].Message, "codex-main") {
		t.Fatalf("findings = %+v, want one cannot-update warning for codex-main", got)
	}
}

const reading1140 = `"last_good":{"taken_at":"2026-10-22T11:40:00Z","windows":[]}`
