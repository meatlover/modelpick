package archtest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestDeployLocal runs the real scripts/deploy-local.sh, which builds three
// binaries, into a temp install dir. It runs it through a relative symlink
// from another directory, so the script must find the repo from its real
// path.
func TestDeployLocal(t *testing.T) {
	if testing.Short() {
		t.Skip("builds three binaries")
	}
	dir, home := t.TempDir(), t.TempDir()
	// A fresh HOME would give go an empty build cache; pin the HOME-derived
	// go dirs first, computed once here.
	goEnv := []string{"HOME=" + home, "MODELPICK_INSTALL_DIR=" + dir}
	for _, k := range []string{"GOCACHE", "GOMODCACHE", "GOPATH"} {
		out, err := exec.Command("go", "env", k).Output()
		if err != nil {
			t.Fatal(err)
		}
		goEnv = append(goEnv, k+"="+strings.TrimSpace(string(out)))
	}
	// Under a git hook GIT_DIR points at the real repo; poison it (and the
	// XDG git config) so the script's git calls fail unless env is scrubbed.
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "poisoned"))
	t.Setenv("GIT_INDEX_FILE", filepath.Join(t.TempDir(), "poisoned-index"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
	script, err := filepath.Abs("../../scripts/deploy-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()
	if err := os.Mkdir(filepath.Join(elsewhere, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(script, filepath.Join(elsewhere, "real")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../real", filepath.Join(elsewhere, "bin", "deploy")); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "bin/deploy")
	cmd.Dir = elsewhere
	cmd.Env = append(cleanEnv(), goEnv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("deploy-local.sh: %v\nstderr: %s", err, stderr.String())
	}
	installed := filepath.Join(dir, "modelpick")
	fi, err := os.Stat(installed)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&0o111 == 0 {
		t.Errorf("installed file mode %v is not executable", fi.Mode())
	}
	got, _ := os.ReadFile(installed)
	want, err := os.ReadFile("../../cmd/modelpick/modelpick")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("installed binary differs from cmd/modelpick/modelpick")
	}
	for _, n := range []string{"modelpick-ui-demo", "modelpick-e2e-server"} {
		if _, err := os.Stat("../../cmd/" + n + "/" + n); err != nil {
			t.Error(err)
		}
	}
	line := regexp.MustCompile(`^modelpick: deployed [0-9][^ ]* \([0-9a-f]{7,}\) to ` + regexp.QuoteMeta(installed) + `\n$`)
	if !line.MatchString(stdout.String()) {
		t.Errorf("stdout = %q, want one deployed line", stdout.String())
	}
}

// cleanEnv is the environment without any GIT_* variable or XDG_CONFIG_HOME, so a git call
// resolves its repository from its working directory alone.
func cleanEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") && !strings.HasPrefix(kv, "XDG_CONFIG_HOME=") {
			env = append(env, kv)
		}
	}
	return append(env, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
}

// gitIn runs git in dir with the owner's git config hidden and a test identity.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(cleanEnv(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// hookRepo makes a temp repo on main whose hooks are copies of ours and whose
// scripts/deploy-local.sh is a stub appending a line to the returned marker.
func hookRepo(t *testing.T) (repo, marker string) {
	t.Helper()
	// Under a git hook (pre-commit runs check.sh) GIT_DIR and GIT_INDEX_FILE
	// point at the real repo. Poison them: a git call that does not scrub
	// them then fails instead of committing into the real repository.
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "poisoned"))
	t.Setenv("GIT_INDEX_FILE", filepath.Join(t.TempDir(), "poisoned-index"))
	repo, marker = t.TempDir(), filepath.Join(t.TempDir(), "marker")
	for _, d := range []string{".githooks", "scripts"} {
		if err := os.Mkdir(filepath.Join(repo, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range []string{"post-merge", "post-rewrite"} {
		b, err := os.ReadFile("../../.githooks/" + h)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, ".githooks", h), b, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stub := "#!/usr/bin/env bash\necho ran >> '" + marker + "'\n"
	if err := os.WriteFile(filepath.Join(repo, "scripts", "deploy-local.sh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "init", "-q", "-b", "main")
	gitIn(t, repo, "config", "core.hooksPath", ".githooks")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "base")
	return repo, marker
}

// mergeFeature merges a one-commit branch into the current branch.
func mergeFeature(t *testing.T, repo, name string) {
	t.Helper()
	gitIn(t, repo, "checkout", "-q", "-b", name, "main")
	if err := os.WriteFile(filepath.Join(repo, name), []byte(name), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", name)
	gitIn(t, repo, "commit", "-q", "-m", name)
	gitIn(t, repo, "checkout", "-q", "main")
	gitIn(t, repo, "merge", "-q", "--no-ff", "-m", "merge "+name, name)
}

func ran(marker string) bool {
	_, err := os.Stat(marker)
	return err == nil
}

func TestPostMergeGating(t *testing.T) {
	t.Run("main and opted in deploys", func(t *testing.T) {
		repo, marker := hookRepo(t)
		gitIn(t, repo, "config", "--bool", "modelpick.autodeploy", "true")
		mergeFeature(t, repo, "f1")
		if !ran(marker) {
			t.Error("deploy did not run")
		}
	})
	t.Run("not opted in", func(t *testing.T) {
		repo, marker := hookRepo(t)
		mergeFeature(t, repo, "f1")
		if ran(marker) {
			t.Error("deploy ran without opt-in")
		}
	})
	t.Run("not on main", func(t *testing.T) {
		repo, marker := hookRepo(t)
		gitIn(t, repo, "config", "--bool", "modelpick.autodeploy", "true")
		gitIn(t, repo, "checkout", "-q", "-b", "other")
		mergeFeature2(t, repo)
		if ran(marker) {
			t.Error("deploy ran on a non-main branch")
		}
	})
}

// mergeFeature2 merges a one-commit branch into the current (non-main) branch.
func mergeFeature2(t *testing.T, repo string) {
	t.Helper()
	gitIn(t, repo, "checkout", "-q", "-b", "side")
	if err := os.WriteFile(filepath.Join(repo, "side"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", "side")
	gitIn(t, repo, "commit", "-q", "-m", "side")
	gitIn(t, repo, "checkout", "-q", "other")
	gitIn(t, repo, "merge", "-q", "--no-ff", "-m", "merge side", "side")
}

func TestPostRewriteGating(t *testing.T) {
	for _, tc := range []struct {
		arg  string
		want bool
	}{{"amend", false}, {"rebase", true}} {
		repo, marker := hookRepo(t)
		gitIn(t, repo, "config", "--bool", "modelpick.autodeploy", "true")
		cmd := exec.Command("bash", ".githooks/post-rewrite", tc.arg)
		cmd.Dir = repo
		cmd.Env = cleanEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("post-rewrite %s: %v\n%s", tc.arg, err, out)
		}
		if ran(marker) != tc.want {
			t.Errorf("post-rewrite %s: ran = %v, want %v", tc.arg, ran(marker), tc.want)
		}
	}
}

// deployCommands is every command scripts/deploy-local.sh may run. The
// script only installs: it never runs a binary it built and never starts,
// stops or signals a process, so a command not on this list (a built
// binary, kill, launchctl, …) fails TestDeployLocalRunsOnlyKnownCommands.
var deployCommands = map[string]bool{
	"set": true, "command": true, "echo": true, "exit": true, "cd": true, "dirname": true,
	"readlink": true, "[": true, "trap": true, "local": true, "mktemp": true, "go": true,
	"mv": true, "mkdir": true, "cp": true, "chmod": true, "sed": true, "git": true,
	"build_to": true, "build_to()": true, "fi": true, "done": true, "for": true, "}": true,
	"if": true, "then": true, "do": true, "else": true, "while": true, "!": true,
}

// TestDeployLocalRunsOnlyKnownCommands reads the word in command position
// on every line and after each $( ; && || and |. It does not parse shell:
// a command hidden in a string run by eval or trap is not seen, so the
// script uses neither for anything but its cleanup trap.
func TestDeployLocalRunsOnlyKnownCommands(t *testing.T) {
	b, err := os.ReadFile("../../scripts/deploy-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range deployUnknownCommands(string(b)) {
		t.Errorf("deploy-local.sh runs %s, which is not in deployCommands", bad)
	}
}

func TestDeployCommandCheckCatches(t *testing.T) {
	src := "set -e\nx=$(\"$dir/modelpick\" version)\ncmd/modelpick/modelpick ls\nif true; then pkill modelpick; fi\n"
	got := deployUnknownCommands(src)
	want := []string{"$dir/modelpick", "cmd/modelpick/modelpick", "true", "pkill"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("unknown commands %q, want %q", got, want)
	}
}

var (
	shellSeparators = regexp.MustCompile(`\$\(|;|&&|\|\||\|`)
	shellAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\+)?=`)
	shellKeywords   = map[string]bool{"if": true, "then": true, "do": true, "else": true, "while": true, "!": true}
)

func deployUnknownCommands(src string) []string {
	var bad []string
	for _, line := range strings.Split(src, "\n") {
		if i := strings.Index(line, "#"); i >= 0 { // the script has no # inside ${…}
			line = line[:i]
		}
		for _, seg := range shellSeparators.Split(line, -1) {
			for _, w := range strings.Fields(seg) {
				w = strings.Trim(w, `"'`)
				if shellAssignment.MatchString(w) {
					continue
				}
				if !deployCommands[w] {
					bad = append(bad, w)
				}
				if !shellKeywords[w] {
					break
				}
			}
		}
	}
	return bad
}
