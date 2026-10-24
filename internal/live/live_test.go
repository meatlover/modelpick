//go:build live

// Package live holds the opt-in smoke test against the real vendor endpoints.
// It is excluded from `go test ./...`; run it by hand:
//
//	go test -tags live ./internal/live/ -v
package live

import (
	"context"
	"errors"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/cli"
	"github.com/meatlover/modelpick/internal/core"
	"github.com/meatlover/modelpick/internal/platform"
	"github.com/meatlover/modelpick/internal/usage"
)

// TestLiveUsage reads every configured account through the real store with
// refresh=false, so the call floor still applies. It prints only the account
// name, window lengths and percents. It never renews an expired login unless
// MODELPICK_LIVE_RENEW=1: a renewal rotates the real refresh token and spends
// Claude tokens.
func TestLiveUsage(t *testing.T) {
	d := app.DefaultDeps(cli.Version)
	if d.Home == "" {
		t.Fatal("$HOME is not set")
	}
	path := platform.ConfigPath(d.Getenv, d.Home)
	cfg, err := core.Load(path, d.Home)
	if err != nil {
		t.Fatalf("load config %s: %v", path, err)
	}
	store := &usage.Store{
		Dir:         platform.CacheDir(d.Getenv, d.Home),
		Floor:       cfg.Settings.MinCallInterval,
		Now:         time.Now,
		CallTimeout: usage.DefaultCallTimeout,
		NoRenew:     os.Getenv("MODELPICK_LIVE_RENEW") != "1",
	}
	for _, name := range sortedAccounts(cfg) {
		acct := cfg.Accounts[name]
		a, ok := d.Adapters[acct.Vendor]
		if !ok {
			t.Errorf("%s: no adapter for vendor %q", name, acct.Vendor)
			continue
		}
		res := store.Get(context.Background(), a, acct, false)
		if errors.Is(res.Err, adapter.ErrShape) {
			t.Errorf("%s: vendor response shape not recognised", name)
		}
		if res.Reading == nil {
			t.Logf("%s: no reading (%s)", name, kind(res.Err))
			continue
		}
		for _, w := range res.Reading.Windows {
			t.Logf("%s: window %s (%s) %.0f%%", name, w.Label, w.Length, w.UsedPct)
		}
		if res.Err != nil {
			t.Logf("%s: older reading used (%s)", name, kind(res.Err))
		}
	}
}

// kind names an error class without echoing its text, which could carry
// request detail.
func kind(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, adapter.ErrTokenExpired):
		return "token expired"
	case errors.Is(err, adapter.ErrUnauthorized):
		return "unauthorized"
	case errors.Is(err, adapter.ErrShape):
		return "unrecognised shape"
	}
	return "error"
}

func sortedAccounts(cfg *core.Config) []string {
	names := make([]string, 0, len(cfg.Accounts))
	for n := range cfg.Accounts {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
