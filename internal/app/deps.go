// Package app is the wiring shared by the command line and the UI back end:
// the dependencies a command needs from the outside world, the usage
// environment built on them, and the helpers that turn vendor failures into
// advice.
package app

import (
	"errors"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/adapter/claude"
	"github.com/meatlover/modelpick/internal/adapter/codex"
	"github.com/meatlover/modelpick/internal/adapter/opencodego"
	"github.com/meatlover/modelpick/internal/core"
	"github.com/meatlover/modelpick/internal/platform"
)

// Deps is everything a command needs from the outside world.
type Deps struct {
	Getenv   func(string) string
	Home     string // "" = unknown; commands refuse to run
	Now      func() time.Time
	Adapters map[string]adapter.Adapter // keyed by vendor
}

// ErrNoHome is reported instead of running with an empty home directory,
// which would put config, cache and credential paths relative to the cwd.
var ErrNoHome = errors.New("cannot determine the home directory: $HOME is not set")

// DefaultDeps returns the real environment, clock and vendor adapters. version
// goes into the User-Agent of the adapters that send one.
func DefaultDeps(version string) Deps {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "" // callers report ErrNoHome
	}
	client := newHTTPClient()
	ua := "modelpick/" + version
	return Deps{
		Getenv: os.Getenv,
		Home:   home,
		Now:    time.Now,
		Adapters: byVendor(
			// Claude computes its own User-Agent from `claude --version`.
			claude.New(claude.Options{HTTP: client, Home: home, Now: time.Now,
				Keychain: platform.KeychainPassword, Run: platform.ExecRunner, LookPath: exec.LookPath}),
			codex.New(codex.Options{HTTP: client, Now: time.Now, LookPath: exec.LookPath, UserAgent: ua, Version: version}),
			opencodego.New(opencodego.Options{HTTP: client, Now: time.Now,
				Run: platform.ExecRunner, LookPath: exec.LookPath, UserAgent: ua}),
		),
	}
}

// newHTTPClient is shared by every adapter. It never follows a redirect, so
// auth and account headers cannot reach another host; adapters see the 3xx
// as an HTTPError.
func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// byVendor keys adapters by their own Vendor(), so a key cannot disagree
// with the adapter it names.
func byVendor(as ...adapter.Adapter) map[string]adapter.Adapter {
	m := make(map[string]adapter.Adapter, len(as))
	for _, a := range as {
		m[a.Vendor()] = a
	}
	return m
}

// LoadConfig reads the effective config; the error is ready to print.
func LoadConfig(d Deps) (*core.Config, error) {
	if d.Home == "" {
		return nil, ErrNoHome
	}
	return core.Load(platform.ConfigPath(d.Getenv, d.Home), d.Home)
}
