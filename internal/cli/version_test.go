package cli

import (
	"runtime/debug"
	"strings"
	"testing"

	"github.com/meatlover/modelpick/internal/app/apptest"
)

func buildInfo(mainVersion string, settings ...string) *debug.BuildInfo {
	bi := &debug.BuildInfo{Main: debug.Module{Path: "github.com/meatlover/modelpick", Version: mainVersion}}
	for i := 0; i+1 < len(settings); i += 2 {
		bi.Settings = append(bi.Settings, debug.BuildSetting{Key: settings[i], Value: settings[i+1]})
	}
	return bi
}

const rev = "54fbf8226dff94d30f4327b4676119f83f246716"

func TestBuildVersion(t *testing.T) {
	cases := []struct {
		name string
		bi   *debug.BuildInfo
		want string
	}{
		{"local build at a release tag", buildInfo("v0.2.0",
			"vcs.revision", rev, "vcs.time", "2026-10-24T22:16:04Z", "vcs.modified", "false"), "v0.2.0"},
		{"go install at a release tag", buildInfo("v0.2.0"), "v0.2.0"},
		{"local build between tags", buildInfo("v0.2.1-0.20261024221604-54fbf8226dff",
			"vcs.revision", rev, "vcs.time", "2026-10-24T22:16:04Z", "vcs.modified", "false"), "20261024-54fbf8"},
		// The date is the commit's, in UTC: 01:30 UTC is still the evening before in -04.
		{"commit time is read in UTC", buildInfo("v0.2.1-0.20261025013000-54fbf8226dff",
			"vcs.revision", rev, "vcs.time", "2026-10-25T01:30:00Z", "vcs.modified", "false"), "20261025-54fbf8"},
		{"uncommitted changes", buildInfo("v0.2.1-0.20261024221604-54fbf8226dff+dirty",
			"vcs.revision", rev, "vcs.time", "2026-10-24T22:16:04Z", "vcs.modified", "true"), "20261024-54fbf8-dirty"},
		// A toolchain that stamps vcs.* but not the module version (Go < 1.24).
		{"uncommitted changes, vcs.modified only", buildInfo("(devel)",
			"vcs.revision", rev, "vcs.time", "2026-10-24T22:16:04Z", "vcs.modified", "true"), "20261024-54fbf8-dirty"},
		{"tagged commit with uncommitted changes", buildInfo("v0.2.0+dirty",
			"vcs.revision", rev, "vcs.time", "2026-10-24T22:16:04Z", "vcs.modified", "true"), "20261024-54fbf8-dirty"},
		{"pre-release tag", buildInfo("v0.3.0-rc.1",
			"vcs.revision", rev, "vcs.time", "2026-10-24T22:16:04Z", "vcs.modified", "false"), "v0.3.0-rc.1"},
		{"go install at a commit (pseudo-version only)", buildInfo("v0.2.1-0.20261024221604-54fbf8226dff"), "20261024-54fbf8"},
		{"no version control information", buildInfo("(devel)"), "dev"},
		{"no build information at all", nil, "dev"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := buildVersion(c.bi); got != c.want {
				t.Errorf("buildVersion = %q, want %q", got, c.want)
			}
		})
	}
}

func TestVersionCommand(t *testing.T) {
	d := apptest.NewDeps(t, testConfig)
	for _, args := range [][]string{{"version"}, {"--version"}} {
		stdout, stderr, code := run(d, args...)
		if code != 0 || stderr != "" {
			t.Fatalf("%v: exit %d, stderr %q", args, code, stderr)
		}
		if !strings.HasPrefix(stdout, "modelpick ") || !strings.HasSuffix(stdout, "\n") || strings.Count(stdout, "\n") != 1 {
			t.Errorf("%v: stdout = %q, want one line \"modelpick <version>\"", args, stdout)
		}
	}
	if _, _, code := run(d, "version", "extra"); code != 1 {
		t.Errorf("version extra: exit %d, want 1", code)
	}
}
