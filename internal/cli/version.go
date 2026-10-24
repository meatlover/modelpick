package cli

import (
	"fmt"
	"io"
	"regexp"
	"runtime/debug"
	"strings"
	"time"
)

// Version is the modelpick version. A release commit (the one a vN.N.N tag
// points at) holds that release's number; between releases it holds the
// next number with -dev. -ldflags "-X github.com/meatlover/modelpick/internal/cli.Version=…"
// still overrides it. It goes into the User-Agent of usage calls; `modelpick
// version` reports the build instead (buildVersion).
var Version = "0.3.0-dev"

// pseudoVersion matches the timestamp and commit of a Go pseudo-version
// (v0.2.1-0.20261024221604-54fbf8226dff), which a build of an untagged commit
// gets.
var pseudoVersion = regexp.MustCompile(`(?:^|[-.])(\d{14})-([0-9a-f]{12})$`)

// buildVersion names the build: its tag when built clean at a tag (v0.2.0),
// otherwise the commit's UTC date and first 6 hex digits (20261024-54fbf8),
// with -dirty for uncommitted changes. "dev" when the build carries no
// version control information (go run, go test).
func buildVersion(bi *debug.BuildInfo) string {
	if bi == nil {
		return "dev"
	}
	setting := func(key string) string {
		for _, s := range bi.Settings {
			if s.Key == key {
				return s.Value
			}
		}
		return ""
	}
	v, dirty := strings.CutSuffix(bi.Main.Version, "+dirty")
	dirty = dirty || setting("vcs.modified") == "true"
	pseudo := pseudoVersion.FindStringSubmatch(v)
	if !dirty && pseudo == nil && strings.HasPrefix(v, "v") {
		return v
	}
	var date, rev string
	if t, err := time.Parse(time.RFC3339, setting("vcs.time")); err == nil && len(setting("vcs.revision")) >= 6 {
		date, rev = t.UTC().Format("20060102"), setting("vcs.revision")
	} else if pseudo != nil {
		date, rev = pseudo[1][:8], pseudo[2]
	} else {
		return "dev"
	}
	out := date + "-" + rev[:6]
	if dirty {
		out += "-dirty"
	}
	return out
}

// runVersion is `modelpick version` and `modelpick --version`.
func runVersion(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		return badUsage(stderr, fmt.Sprintf("version: unexpected argument %q", args[0]))
	}
	bi, _ := debug.ReadBuildInfo()
	fmt.Fprintf(stdout, "modelpick %s\n", buildVersion(bi))
	return 0
}
