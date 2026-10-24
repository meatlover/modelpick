package app_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/app/apptest"
	"github.com/meatlover/modelpick/internal/core"
)

// TestEnvMemoisesPerAccount checks the env's own memo, with a zero floor so
// neither the store nor core.Pick could be what prevents the second call.
func TestEnvMemoisesPerAccount(t *testing.T) {
	cfgJSON := strings.Replace(apptest.Config, `"version": 1,`,
		`"version": 1, "settings": { "min_call_interval_minutes": 0 },`, 1)
	claude := apptest.NewFake("claude")
	claude.Readings["claude-main"] = apptest.ClaudeReading(10, 10)
	d := apptest.NewDeps(t, cfgJSON, claude)
	cfg, err := core.Load(d.Getenv("MODELPICK_CONFIG"), d.Home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Settings.MinCallInterval != 0 {
		t.Fatalf("test setup: floor = %v, want 0", cfg.Settings.MinCallInterval)
	}
	e := app.NewEnv(d, cfg, false)

	for range 2 {
		if c := e.Credential("claude-main"); !c.Present {
			t.Fatalf("credential = %+v", c)
		}
		if st := e.Usage("claude-main"); st.Reading == nil || st.Err != nil {
			t.Fatalf("usage = %+v", st)
		}
	}
	if got := claude.Calls["claude-main"]; got != 1 {
		t.Fatalf("Usage called %d times, want 1", got)
	}
	// The store asks the adapter once more itself (expiry check), so two
	// calls: one from the env's memo, one from the single Get.
	if got := claude.CredCalls["claude-main"]; got != 2 {
		t.Fatalf("Credential called %d times, want 2 (env once, store once)", got)
	}

	// A second env (a second pick) does call again: the floor is zero.
	app.NewEnv(d, cfg, false).Usage("claude-main")
	if got := claude.Calls["claude-main"]; got != 2 {
		t.Fatalf("a new env made %d total calls, want 2", got)
	}
}

func TestUsageErrorMessages(t *testing.T) {
	wrapped := func(e error) error { return errors.Join(errors.New("vendor"), e) }
	cases := []struct {
		vendor string
		err    error
		want   string
	}{
		{"claude", wrapped(adapter.ErrUnauthorized), "usage endpoint returned HTTP 401; log in to Claude Code with this account"},
		{"codex", wrapped(adapter.ErrUnauthorized), "usage endpoint returned HTTP 401; log in to Codex with this account"},
		{"opencode-go", wrapped(adapter.ErrUnauthorized), "usage endpoint returned HTTP 401; log in to OpenCode with this account"},
		{"claude", adapter.ErrTokenExpired, "token expired; open Claude Code with this account to refresh it"},
		{"codex", adapter.ErrTokenExpired, "token expired; open Codex with this account to refresh it"},
		{"claude", &adapter.HTTPError{Status: 503}, "usage endpoint returned HTTP 503"},
	}
	for _, c := range cases {
		got := app.UsageError(c.vendor, c.err)
		if got.Error() != c.want {
			t.Errorf("%s %v: got %q, want %q", c.vendor, c.err, got.Error(), c.want)
		}
		if !errors.Is(got, c.err) {
			t.Errorf("%s %v: rewritten error no longer matches the original", c.vendor, c.err)
		}
	}
}

func TestDefaultDepsAdaptersKeyedByVendor(t *testing.T) {
	d := app.DefaultDeps("test")
	want := []string{"claude", "codex", "opencode-go"}
	if len(d.Adapters) != len(want) {
		t.Fatalf("adapters = %v, want %v", d.Adapters, want)
	}
	for _, v := range want {
		a, ok := d.Adapters[v]
		if !ok || a.Vendor() != v {
			t.Fatalf("Adapters[%q] = %v", v, a)
		}
	}
	if d.Getenv == nil || d.Now == nil {
		t.Fatal("DefaultDeps left Getenv or Now nil")
	}
}
