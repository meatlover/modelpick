package main

import (
	"bytes"
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/core"
	"github.com/meatlover/modelpick/internal/usage"
)

func fixture(t *testing.T) (app.Deps, *core.Config) {
	t.Helper()
	d, fakes, err := setup(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(fakes) != 3 {
		t.Fatalf("want fakes for 3 vendors, got %d", len(fakes))
	}
	cfg, err := app.LoadConfig(d)
	if err != nil {
		t.Fatal(err)
	}
	return d, cfg
}

func TestE2EFixtureIsClean(t *testing.T) {
	d, cfg := fixture(t)
	if got := app.Doctor(cfg, d, nil); len(got) != 0 {
		t.Fatalf("fixture has doctor findings: %+v", got)
	}
	if len(cfg.Accounts) != len(accountVendor) {
		t.Fatalf("config has %d accounts, accountVendor %d", len(cfg.Accounts), len(accountVendor))
	}
	for n, a := range cfg.Accounts {
		if accountVendor[n] != a.Vendor {
			t.Errorf("%s: config vendor %q, accountVendor %q", n, a.Vendor, accountVendor[n])
		}
	}
}

// TestE2EFixtureStoredAges: every account has a stored reading, and only
// codex-main's is older than the floor, so Check all calls exactly it.
func TestE2EFixtureStoredAges(t *testing.T) {
	d, cfg := fixture(t)
	st := app.NewStore(d, cfg)
	for n, a := range cfg.Accounts {
		s, err := st.Saved(a)
		if err != nil || s.Reading == nil {
			t.Fatalf("%s: no stored reading: %v", n, err)
		}
		old := usage.Age(s.Reading.TakenAt, d.Now()) >= cfg.Settings.MinCallInterval
		if old != (n == "codex-main") {
			t.Errorf("%s: older than the floor = %v", n, old)
		}
	}
}

func TestCommands(t *testing.T) {
	d, fakes, err := setup(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := app.LoadConfig(d)
	if err != nil {
		t.Fatal(err)
	}
	in := "fail codex-main boom bang\nfail nobody x\ntool opencode-go missing\nfrobnicate\n"
	var out bytes.Buffer
	readCommands(strings.NewReader(in), &out, d, fakes, new(atomic.Bool))
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 || lines[0] != "ok" || !strings.HasPrefix(lines[1], "error ") ||
		lines[2] != "ok" || !strings.HasPrefix(lines[3], "error unknown command") {
		t.Fatalf("answers: %q", lines)
	}
	ctx := context.Background()
	if _, err := fakes["codex"].Usage(ctx, cfg.Accounts["codex-main"]); err == nil || err.Error() != "boom bang" {
		t.Fatalf("fail: Usage error %v, want boom bang", err)
	}
	for _, n := range []string{"go-1", "go-2"} {
		fs := fakes["opencode-go"].Check(ctx, cfg.Accounts[n])
		if len(fs) != 1 || fs[0].Message != "opencode not found on PATH" || fs[0].Severity != adapter.SeverityError {
			t.Fatalf("tool missing: %s Check = %+v", n, fs)
		}
		if _, err := fakes["opencode-go"].Models(ctx, cfg.Accounts[n]); err == nil {
			t.Fatalf("tool missing: %s still lists models", n)
		}
	}

	out.Reset()
	readCommands(strings.NewReader("clear codex-main\ntool opencode-go present\n"), &out, d, fakes, new(atomic.Bool))
	if out.String() != "ok\nok\n" {
		t.Fatalf("answers: %q", out.String())
	}
	if _, err := fakes["codex"].Usage(ctx, cfg.Accounts["codex-main"]); err != nil {
		t.Fatalf("clear: Usage error %v", err)
	}
	if fs := fakes["opencode-go"].Check(ctx, cfg.Accounts["go-1"]); len(fs) != 0 {
		t.Fatalf("tool present: Check = %+v", fs)
	}
	if ms, err := fakes["opencode-go"].Models(ctx, cfg.Accounts["go-1"]); err != nil || len(ms) == 0 {
		t.Fatalf("tool present: Models = %v, %v", ms, err)
	}
}

func TestSetupRefusesNonEmptyDir(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := setup(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := setup(dir, time.Now()); err == nil {
		t.Fatal("setup reused a non-empty dir")
	}
}

// TestCommandsBesideCalls: commands mutate the fakes while the server's
// handlers call them; run with -race, this fails on an unguarded access.
func TestCommandsBesideCalls(t *testing.T) {
	d, fakes, err := setup(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := app.LoadConfig(d)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			_, _ = fakes["codex"].Usage(ctx, cfg.Accounts["codex-main"])
			_ = fakes["opencode-go"].Check(ctx, cfg.Accounts["go-1"])
			_, _ = fakes["opencode-go"].Models(ctx, cfg.Accounts["go-1"])
		}
	}()
	in := strings.Repeat("fail codex-main boom\nclear codex-main\ntool opencode-go missing\ntool opencode-go present\n", 50)
	var out bytes.Buffer
	readCommands(strings.NewReader(in), &out, d, fakes, new(atomic.Bool))
	<-done
	if n := strings.Count(out.String(), "ok\n"); n != 200 {
		t.Fatalf("want 200 ok, got %d: %q", n, out.String())
	}
}

func TestBinaryCommandFlipsTheReportedValue(t *testing.T) {
	d, fakes, err := setup(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var replaced atomic.Bool
	var out bytes.Buffer
	readCommands(strings.NewReader("binary replaced\n"), &out, d, fakes, &replaced)
	if out.String() != "ok\n" || !replaced.Load() {
		t.Fatalf("binary replaced: answer %q, flag %v", out.String(), replaced.Load())
	}
	out.Reset()
	readCommands(strings.NewReader("binary current\nbinary sideways\n"), &out, d, fakes, &replaced)
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || lines[0] != "ok" || !strings.HasPrefix(lines[1], "error unknown command") || replaced.Load() {
		t.Fatalf("binary current: answers %q, flag %v", lines, replaced.Load())
	}
}
