package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/meatlover/modelpick/internal/core"
	"github.com/meatlover/modelpick/internal/platform"
)

// runPick is `modelpick pick <profile> [--refresh]`: the result JSON on
// stdout, its exit code returned.
func runPick(args []string, stdout, stderr io.Writer, d Deps) int {
	profile, refresh, err := parsePickArgs(args)
	if err != nil {
		return badUsage(stderr, err.Error())
	}
	res := pick(profile, refresh, d)
	_, _ = stdout.Write(res.JSON())
	return res.ExitCode()
}

func parsePickArgs(args []string) (profile string, refresh bool, err error) {
	for _, a := range args {
		switch {
		case a == "--refresh":
			refresh = true
		case strings.HasPrefix(a, "-"):
			return "", false, fmt.Errorf("pick: unknown flag %q", a)
		case profile != "":
			return "", false, fmt.Errorf("pick: unexpected argument %q", a)
		default:
			profile = a
		}
	}
	if profile == "" {
		return "", false, fmt.Errorf("pick: missing <profile>")
	}
	return profile, refresh, nil
}

func pick(profile string, refresh bool, d Deps) core.Result {
	if d.Home == "" {
		return core.Failed(profile, errNoHome.Error())
	}
	cfg, err := core.Load(platform.ConfigPath(d.Getenv, d.Home), d.Home)
	if err != nil {
		return core.Failed(profile, err.Error())
	}
	return core.Pick(cfg, profile, d.Now(), newEnv(d, cfg, refresh))
}
