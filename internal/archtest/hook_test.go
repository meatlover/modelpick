package archtest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// needsE2E runs the real scripts/e2e-needed.sh, the classifier the pre-commit
// hook uses, on NUL-separated staged paths. true means "run the browser tests".
func needsE2E(t *testing.T, paths ...string) bool {
	t.Helper()
	script, err := filepath.Abs("../../scripts/e2e-needed.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script)
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\x00") + "\x00")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	switch err := cmd.Run().(type) {
	case nil:
		return true
	case *exec.ExitError:
		if err.ExitCode() == 1 {
			return false
		}
	}
	t.Fatalf("e2e-needed.sh %q: unexpected failure: %s", paths, stderr.String())
	return false
}

// TestE2ENeededClassifier: only paths a commit can use to change the browser
// tests' outcome (the real back end, the static UI, the tests, the scripts
// and hooks that run them, the module files) make them run.
func TestE2ENeededClassifier(t *testing.T) {
	skip := [][]string{
		{"docs/x.md"},
		{"README.md"},
		{".github/workflows/ci.yml"},
		{"docs/x.md", "CHANGELOG.md", ".github/workflows/release.yml"},
		{"internalx/a.go"}, // a prefix match must end at the slash
		{"docs/internal/a.go"},
	}
	for _, c := range skip {
		if needsE2E(t, c...) {
			t.Errorf("%q: classifier says run, want skip", c)
		}
	}
	run := [][]string{
		{"internal/web/static/app.js"},
		{"cmd/modelpick/main.go"},
		{"e2e/tests/a.spec.js"},
		{"scripts/check.sh"},
		{".githooks/pre-commit"},
		{"go.mod"},
		{"go.sum"},
		{"docs/x.md", "internal/core/a.go"},
		{"internal/core/é.go"},
	}
	for _, c := range run {
		if !needsE2E(t, c...) {
			t.Errorf("%q: classifier says skip, want run", c)
		}
	}
}

// TestPreCommitHookSkipsBrowserTests runs the real hook in a throwaway repo
// whose check.sh only reports the knob it saw, once with a docs-only commit
// staged and once with a Go file staged, and with the knob already set in
// the caller's environment (the hook must clear it when it does not skip).
func TestPreCommitHookSkipsBrowserTests(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, staged, inherited, wantKnob, wantLine string
	}{
		{"docs only", "docs/x.md", "", "knob=1", "pre-commit: browser tests skipped"},
		{"go file", "internal/core/a.go", "", "knob=", ""},
		{"go file, knob inherited", "internal/core/a.go", "1", "knob=", ""},
		{"renamed out of internal", "internal/old.go", "", "knob=", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := t.TempDir()
			for _, d := range []string{".githooks", "scripts"} {
				if err := os.Mkdir(filepath.Join(repo, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for _, f := range []string{".githooks/pre-commit", "scripts/e2e-needed.sh"} {
				b, err := os.ReadFile(filepath.Join(root, f))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(repo, f), b, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			stub := "#!/usr/bin/env bash\necho \"knob=${MODELPICK_CHECK_SKIP_E2E:-}\"\n"
			if err := os.WriteFile(filepath.Join(repo, "scripts/check.sh"), []byte(stub), 0o755); err != nil {
				t.Fatal(err)
			}
			gitIn(t, repo, "init", "-q")
			if c.name == "renamed out of internal" {
				// The old path of a rename is a change under internal/ too.
				mustWrite(t, repo, c.staged, "package x\n")
				gitIn(t, repo, "add", "-A")
				gitIn(t, repo, "commit", "-q", "-m", "base")
				gitIn(t, repo, "mv", c.staged, "moved.txt")
			} else {
				mustWrite(t, repo, c.staged, "x\n")
				gitIn(t, repo, "add", c.staged)
			}
			cmd := exec.Command("bash", ".githooks/pre-commit")
			cmd.Dir = repo
			cmd.Env = append(cleanEnv(), "MODELPICK_CHECK_SKIP_E2E="+c.inherited)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("hook: %v\n%s", err, out)
			}
			if !strings.Contains(string(out), c.wantKnob+"\n") {
				t.Errorf("want %q in output, got:\n%s", c.wantKnob, out)
			}
			if c.wantLine != "" && !strings.Contains(string(out), c.wantLine) {
				t.Errorf("want skip line %q in output, got:\n%s", c.wantLine, out)
			}
			if c.wantLine == "" && strings.Contains(string(out), "skipped") {
				t.Errorf("unexpected skip line:\n%s", out)
			}
		})
	}
}

func mustWrite(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCheckScriptHonoursSkipKnob runs the real scripts/check.sh with a fake
// `go` on PATH and a stub e2e.sh. With MODELPICK_CHECK_SKIP_E2E=1 it prints
// the skip line and never calls e2e.sh; without it, e2e.sh runs.
func TestCheckScriptHonoursSkipKnob(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, knob := range []string{"1", ""} {
		repo, bin := t.TempDir(), t.TempDir()
		mustWrite(t, repo, "scripts/targets", "linux/amd64\n")
		b, err := os.ReadFile(filepath.Join(root, "scripts/check.sh"))
		if err != nil {
			t.Fatal(err)
		}
		mustWrite(t, repo, "scripts/check.sh", string(b))
		mustWrite(t, repo, "scripts/e2e.sh", "echo STUB-E2E-RAN\n")
		mustWrite(t, bin, "go", "#!/bin/sh\nexit 0\n")
		if err := os.Chmod(filepath.Join(bin, "go"), 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("bash", "scripts/check.sh")
		cmd.Dir = repo
		cmd.Env = append(cleanEnv(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
			"MODELPICK_CHECK_SKIP_E2E="+knob)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("knob=%q: check.sh: %v\n%s", knob, err, out)
		}
		ran := strings.Contains(string(out), "STUB-E2E-RAN")
		skipped := strings.Contains(string(out), "browser tests skipped")
		if knob == "1" && (ran || !skipped) {
			t.Errorf("knob=1: want skip line and no e2e, got:\n%s", out)
		}
		if knob == "" && (!ran || skipped) {
			t.Errorf("knob unset: want e2e to run, got:\n%s", out)
		}
	}
}
