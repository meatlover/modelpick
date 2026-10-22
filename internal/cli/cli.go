// Package cli wires configuration, usage store and vendor adapters into the
// modelpick commands.
package cli

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/adapter/claude"
	"github.com/meatlover/modelpick/internal/adapter/codex"
	"github.com/meatlover/modelpick/internal/adapter/opencodego"
	"github.com/meatlover/modelpick/internal/platform"
)

// Deps is everything a command needs from the outside world.
type Deps struct {
	Getenv   func(string) string
	Home     string // "" = unknown; commands refuse to run
	Now      func() time.Time
	Adapters map[string]adapter.Adapter // keyed by vendor
}

// errNoHome is reported instead of running with an empty home directory,
// which would put config, cache and credential paths relative to the cwd.
var errNoHome = errors.New("cannot determine the home directory: $HOME is not set")

const usageText = `usage:
  modelpick pick <profile> [--refresh]
  modelpick ls [--refresh] [--json]
  modelpick ls models [--json]
  modelpick config
  modelpick doctor
`

// DefaultDeps returns the real environment, clock and vendor adapters.
func DefaultDeps() Deps {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "" // Main reports errNoHome
	}
	client := newHTTPClient()
	ua := "modelpick/" + Version
	return Deps{
		Getenv: os.Getenv,
		Home:   home,
		Now:    time.Now,
		Adapters: byVendor(
			// Claude computes its own User-Agent from `claude --version`.
			claude.New(claude.Options{HTTP: client, Home: home, Now: time.Now,
				Keychain: platform.KeychainPassword, Run: platform.ExecRunner, LookPath: exec.LookPath}),
			codex.New(codex.Options{HTTP: client, Now: time.Now, LookPath: exec.LookPath, UserAgent: ua}),
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

// Main runs the command in args (without the program name) and returns the
// process exit code.
func Main(args []string, stdout, stderr io.Writer, d Deps) int {
	if len(args) == 0 {
		return badUsage(stderr, "missing command")
	}
	switch args[0] {
	case "pick":
		return runPick(args[1:], stdout, stderr, d)
	case "ls":
		return runLs(args[1:], stdout, stderr, d)
	case "config":
		return runConfig(args[1:], stdout, stderr, d)
	case "doctor":
		return runDoctor(args[1:], stdout, stderr, d)
	}
	return badUsage(stderr, fmt.Sprintf("unknown command %q", args[0]))
}

// badUsage prints msg and the usage text on stderr and returns exit 1.
func badUsage(stderr io.Writer, msg string) int {
	fmt.Fprintf(stderr, "modelpick: %s\n\n%s", msg, usageText)
	return 1
}
