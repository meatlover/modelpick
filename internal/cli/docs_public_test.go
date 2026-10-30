package cli

import (
	"os/exec"
	"strings"
	"testing"
)

// trackedOutsideHistory lists the files git tracks, minus the design history
// (docs/superpowers/), which is not published.
func trackedOutsideHistory(t *testing.T) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = "../.."
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	var files []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" && !strings.HasPrefix(f, "docs/superpowers/") {
			files = append(files, f)
		}
	}
	if len(files) < 100 {
		t.Fatalf("git ls-files returned only %d files; the listing may be broken", len(files))
	}
	return files
}

// mentions reports the tracked files (minus exempt) whose text contains needle.
func mentions(t *testing.T, needle string, exempt ...string) []string {
	t.Helper()
	var hits []string
files:
	for _, f := range trackedOutsideHistory(t) {
		for _, e := range exempt {
			if f == e {
				continue files
			}
		}
		if strings.Contains(readDoc(t, "../../"+f), needle) {
			hits = append(hits, f)
		}
	}
	return hits
}

// TestNoAuthorAccountName: the example account is claude-secondary. The
// author's own account name must not come back through a copied fixture; the
// changelog may name it when it records the rename. (The name is built from
// two pieces so this file does not match itself.)
func TestNoAuthorAccountName(t *testing.T) {
	if hits := mentions(t, "claude-"+"rec", "CHANGELOG.md"); len(hits) > 0 {
		t.Errorf("the author's account name appears in: %v", hits)
	}
}

// TestNothingDependsOnDesignHistory: docs/superpowers/ is not published, so no
// other tracked file may point into it. docs/release.md says it is not
// published, which names the path, and this file's comments do too; .gitignore
// and CLAUDE.md name it only to keep it out. Those are the exceptions.
func TestNothingDependsOnDesignHistory(t *testing.T) {
	if hits := mentions(t, "docs/super"+"powers", "docs/release.md", "internal/cli/docs_public_test.go", ".gitignore", "CLAUDE.md"); len(hits) > 0 {
		t.Errorf("files that reference the unpublished design history: %v", hits)
	}
}

// TestDesignHistoryIsNotTracked: design specs, plans and mockups live in the
// untracked .superpowers/ (owner, 2026-10-30), never in the repository.
// .gitignore makes a plain `git add` skip the old path; this catches a
// forced add.
func TestDesignHistoryIsNotTracked(t *testing.T) {
	cmd := exec.Command("git", "ls-files", "docs/super"+"powers")
	cmd.Dir = "../.."
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	if s := strings.TrimSpace(string(out)); s != "" {
		t.Errorf("design history is tracked; it belongs in .superpowers/ (untracked):\n%s", s)
	}
}
