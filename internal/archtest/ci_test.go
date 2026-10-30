package archtest

import (
	"os"
	"strings"
	"testing"
)

// TestCIWorkflowTokenIsReadOnly: the workflow declares a top-level
// `permissions: contents: read`, so its GITHUB_TOKEN cannot write.
func TestCIWorkflowTokenIsReadOnly(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	for i, l := range lines {
		if l == "permissions:" {
			if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == "contents: read" &&
				strings.HasPrefix(lines[i+1], " ") && !strings.Contains(string(b), "write") {
				return
			}
			t.Fatalf("ci.yml: top-level permissions must be exactly `contents: read`, got %q", lines[i+1:])
		}
	}
	t.Fatal("ci.yml has no top-level `permissions:` block")
}

// TestCIWorkflowSkipsTags: ci.yml runs on branch pushes only. A vX.Y.Z tag
// runs check.sh in release.yml; ci.yml on the same tag would repeat it on
// both runners.
func TestCIWorkflowSkipsTags(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "\n  push:\n    branches: ['**']\n") {
		t.Fatal("ci.yml: `on: push:` must be limited to `branches: ['**']`, so a tag push does not run it")
	}
}
