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
// binaries, into a temp install dir.
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
	cmd := exec.Command("bash", "../../scripts/deploy-local.sh")
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
