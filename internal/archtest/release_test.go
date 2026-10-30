package archtest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// goCacheEnv pins the HOME-derived go dirs, so a run under a fresh HOME keeps
// the build cache and module cache.
func goCacheEnv(t *testing.T) []string {
	t.Helper()
	env := []string{"HOME=" + t.TempDir()}
	for _, k := range []string{"GOCACHE", "GOMODCACHE", "GOPATH", "GOOS", "GOARCH"} {
		out, err := exec.Command("go", "env", k).Output()
		if err != nil {
			t.Fatal(err)
		}
		env = append(env, k+"="+strings.TrimSpace(string(out)))
	}
	return env
}

func hostTarget(t *testing.T) string {
	t.Helper()
	var parts []string
	for _, k := range []string{"GOOS", "GOARCH"} {
		out, err := exec.Command("go", "env", k).Output()
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, strings.TrimSpace(string(out)))
	}
	return strings.Join(parts, "/")
}

// copyWorkingTree copies tracked and untracked-not-ignored files of this
// repository's working tree into dst, so the test exercises the scripts as
// edited, before they are committed.
func copyWorkingTree(t *testing.T, dst string) {
	t.Helper()
	cmd := exec.Command("git", "ls-files", "-co", "--exclude-standard", "-z")
	cmd.Dir = "../.."
	cmd.Env = cleanEnv()
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" {
			continue
		}
		src := filepath.Join("../..", rel)
		fi, err := os.Lstat(src)
		if err != nil || !fi.Mode().IsRegular() {
			continue // deleted in the working tree, or a symlink
		}
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		to := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(to, b, fi.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
	}
}

var versionLine = regexp.MustCompile(`(?m)^var Version = ".*"$`)

// releaseRepo is a committed git repo made from the working tree whose
// Version is version and whose CHANGELOG holds a `## v0.0.1-test` section
// when withSection is set, tagged v0.0.1-test (annotated).
func releaseRepo(t *testing.T, withSection bool) string {
	t.Helper()
	repo := t.TempDir()
	copyWorkingTree(t, repo)
	vf := filepath.Join(repo, "internal/cli/version.go")
	b, err := os.ReadFile(vf)
	if err != nil {
		t.Fatal(err)
	}
	b = versionLine.ReplaceAll(b, []byte(`var Version = "0.0.1-test"`))
	if err := os.WriteFile(vf, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if withSection {
		cf := filepath.Join(repo, "CHANGELOG.md")
		c, err := os.ReadFile(cf)
		if err != nil {
			t.Fatal(err)
		}
		c = bytes.Replace(c, []byte("# Changelog\n"), []byte("# Changelog\n\n## v0.0.1-test — 2026-01-01\n\nthe test release line\n"), 1)
		if err := os.WriteFile(cf, c, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, repo, "init", "-q", "-b", "main")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "release: v0.0.1-test")
	gitIn(t, repo, "tag", "-a", "-m", "v0.0.1-test", "v0.0.1-test")
	return repo
}

// runRelease runs the repo's scripts/release-build.sh for the host target
// only (CI's real run covers all four) and returns exit code and stderr.
func runRelease(t *testing.T, repo, tag, out string) (code int, stderr string) {
	t.Helper()
	cmd := exec.Command("bash", "scripts/release-build.sh", tag, out)
	cmd.Dir = repo
	cmd.Env = append(cleanEnv(), goCacheEnv(t)...)
	cmd.Env = append(cmd.Env, "RELEASE_TARGETS="+hostTarget(t))
	var se bytes.Buffer
	cmd.Stderr = &se
	err := cmd.Run()
	if err == nil {
		return 0, se.String()
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), se.String()
	}
	t.Fatal(err)
	return -1, ""
}

func TestReleaseBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary in a scratch repo")
	}
	repo := releaseRepo(t, true)
	// The output directory sits inside the repo, as dist/ does in CI: the
	// script must not leave the tree dirty for the go build that stamps it.
	if code, stderr := runRelease(t, repo, "v0.0.1-test", "dist"); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr)
	}
	dist := filepath.Join(repo, "dist")
	target := strings.ReplaceAll(hostTarget(t), "/", "_")
	archive := filepath.Join(dist, "modelpick_0.0.1-test_"+target+".tar.gz")
	if got := tarNames(t, archive); strings.Join(got, ",") != "CHANGELOG.md,LICENSE,README.md,modelpick" {
		t.Errorf("archive holds %q, want exactly CHANGELOG.md, LICENSE, README.md, modelpick", got)
	}
	sums, err := os.ReadFile(filepath.Join(dist, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(sums), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("SHA256SUMS = %q, want one line", sums)
	}
	hash, name, ok := strings.Cut(lines[0], "  ")
	if !ok || name != filepath.Base(archive) {
		t.Fatalf("SHA256SUMS line %q, want `<hash>  %s`", lines[0], filepath.Base(archive))
	}
	ab, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256(ab); hex.EncodeToString(sum[:]) != hash {
		t.Errorf("SHA256SUMS hash %s does not match the archive", hash)
	}
	notes, err := os.ReadFile(filepath.Join(dist, "notes.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(notes), "the test release line") || strings.Contains(string(notes), "## ") {
		t.Errorf("notes.md = %q, want the section line and no heading", notes)
	}
}

func tarNames(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
	}
	sort.Strings(names)
	return names
}

// TestReleaseBuildGates: each bad input fails at its own step, named in
// stderr, and leaves no archive behind.
func TestReleaseBuildGates(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the release script in scratch repos")
	}
	cases := []struct {
		name string
		prep func(t *testing.T) (repo, tag string)
		step string
	}{
		{"malformed tag", func(t *testing.T) (string, string) { return releaseRepo(t, true), "v1.2" }, "release-build: tag: "},
		{"version.go disagrees with the tag", func(t *testing.T) (string, string) {
			repo := releaseRepo(t, true)
			gitIn(t, repo, "tag", "-a", "-m", "v0.0.2-test", "v0.0.2-test") // same commit: HEAD is the tag
			return repo, "v0.0.2-test"
		}, "release-build: version gate: "},
		{"changelog section missing", func(t *testing.T) (string, string) { return releaseRepo(t, false), "v0.0.1-test" }, "release-build: notes: "},
		{"dirty tree", func(t *testing.T) (string, string) {
			repo := releaseRepo(t, true)
			if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("edited\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return repo, "v0.0.1-test"
		}, "release-build: clean tree: "},
		{"HEAD is not the tag", func(t *testing.T) (string, string) {
			repo := releaseRepo(t, true)
			gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "after the tag")
			return repo, "v0.0.1-test"
		}, "release-build: clean tree: "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, tag := tc.prep(t)
			out := filepath.Join(t.TempDir(), "out")
			code, stderr := runRelease(t, repo, tag, out)
			if code == 0 {
				t.Fatalf("exit 0, want failure; stderr:\n%s", stderr)
			}
			if !strings.Contains(stderr, tc.step) {
				t.Errorf("stderr does not name the step %q:\n%s", tc.step, stderr)
			}
			if m, _ := filepath.Glob(filepath.Join(out, "*.tar.gz")); len(m) > 0 {
				t.Errorf("archives left behind: %v", m)
			}
		})
	}
}

// TestTargetsListHasOneReader: scripts/targets is the only place the build
// targets are spelled out; check.sh and release-build.sh read it.
func TestTargetsListHasOneReader(t *testing.T) {
	tb, err := os.ReadFile("../../scripts/targets")
	if err != nil {
		t.Fatal(err)
	}
	want := "linux/amd64\nlinux/arm64\ndarwin/amd64\ndarwin/arm64\n"
	if string(tb) != want {
		t.Errorf("scripts/targets = %q, want %q", tb, want)
	}
	for _, f := range []string{"check.sh", "release-build.sh"} {
		b, err := os.ReadFile("../../scripts/" + f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		for _, tg := range strings.Fields(want) {
			// release-build.sh's header comment may name RELEASE_TARGETS
			// examples, but not a real target.
			if strings.Contains(src, tg) {
				t.Errorf("%s spells out %s; read scripts/targets instead", f, tg)
			}
		}
		if !strings.Contains(src, "scripts/targets") && !strings.Contains(src, "/targets") {
			t.Errorf("%s does not read scripts/targets", f)
		}
	}
}

// TestReleaseWorkflowPermissions: the token is read-only at the top, and
// `contents: write` appears once, under the release job.
func TestReleaseWorkflowPermissions(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	top := false
	for i, l := range lines {
		if l == "permissions:" && i+1 < len(lines) && lines[i+1] == "  contents: read" {
			top = true
		}
	}
	if !top {
		t.Error("release.yml has no top-level `permissions:` with `contents: read`")
	}
	jobRe := regexp.MustCompile(`^  ([A-Za-z0-9_-]+):\s*$`)
	var writes int
	inJobs, job := false, ""
	for _, l := range lines {
		if strings.HasPrefix(l, "jobs:") {
			inJobs = true
		} else if inJobs && l != "" && !strings.HasPrefix(l, " ") {
			inJobs = false
		}
		if m := jobRe.FindStringSubmatch(l); inJobs && m != nil {
			job = m[1]
		}
		if strings.Contains(l, "contents: write") {
			writes++
			if job != "release" {
				t.Errorf("`contents: write` under job %q, want release", job)
			}
		}
	}
	if writes != 1 {
		t.Errorf("`contents: write` appears %d times, want once", writes)
	}
	if !strings.Contains(string(b), "needs: check") {
		t.Error("release job must need: check")
	}
}
