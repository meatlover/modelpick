package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/core"
	"github.com/meatlover/modelpick/internal/platform"
)

// runPick is `modelpick pick [<profile>] [--refresh]`: with a profile, the
// result JSON on stdout; with no positional at all, the table of every
// profile (runPickAll).
// The exit code is returned.
func runPick(args []string, stdout, stderr io.Writer, d app.Deps) int {
	profile, given, refresh, err := parseProfileArgs("pick", args)
	if err == nil && given && profile == "" {
		// v1's answer to `pick "$PROFILE"` with an empty variable.
		err = errors.New("pick: missing <profile>")
	}
	if err != nil {
		// A launcher reads stdout: it gets a failed result, the user the usage.
		res := core.Failed(profile, err.Error())
		_, _ = stdout.Write(res.JSON())
		badUsage(stderr, err.Error())
		return res.ExitCode()
	}
	if !given {
		return runPickAll(refresh, stdout, stderr, d)
	}
	res := pick(profile, refresh, d)
	_, _ = stdout.Write(res.JSON())
	return res.ExitCode()
}

// parseProfileArgs parses `<cmd> <profile> [--refresh]`. It returns the
// profile seen so far even on error, so a failed result can name it. given
// reports whether any positional argument was seen, an empty one included:
// a missing profile is not an error here, since pick without a positional
// prints the table while an empty one (`pick ""`) fails as in v1.
func parseProfileArgs(cmd string, args []string) (profile string, given, refresh bool, err error) {
	for _, a := range args {
		switch {
		case a == "--refresh":
			refresh = true
		case strings.HasPrefix(a, "-"):
			return profile, given, false, fmt.Errorf("%s: unknown flag %q", cmd, a)
		case profile != "":
			return profile, given, false, fmt.Errorf("%s: unexpected argument %q", cmd, a)
		default:
			profile, given = a, true
		}
	}
	return profile, given, refresh, nil
}

func pick(profile string, refresh bool, d app.Deps) core.Result {
	if d.Home == "" {
		return core.Failed(profile, app.ErrNoHome.Error())
	}
	cfg, err := core.Load(platform.ConfigPath(d.Getenv, d.Home), d.Home)
	if err != nil {
		return core.Failed(profile, err.Error())
	}
	return core.Pick(cfg, profile, d.Now(), app.NewEnv(d, cfg, refresh))
}
