// Package archtest enforces the import layering between the module's packages.
package archtest

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// allowed lists, per restricted package (path relative to the module root),
// the module packages it may import. A restricted package with an empty list
// imports no module package. cmd/... and internal/cli (the composition
// roots) are unrestricted, and so is internal/app/apptest, the test fakes.
// That exemption is removable by an entry for apptest naming what it
// imports today (internal/app, internal/adapter), so a fake could not reach
// web or cli; deferred because apptest is imported only by _test.go files
// and by cmd/modelpick-e2e-server, the browser tests' server, which only
// scripts/e2e.sh builds, into a temp dir: no release or install path does
// (the README and docs/install.md build and install ./cmd/modelpick only,
// and check.sh's `go build ./...` writes no binary), so it never ships, and
// its imports follow whatever the app, cli, web/live and e2e suites need
// next.
var allowed = map[string][]string{
	"internal/pathx":    {},
	"internal/platform": {"internal/pathx"},
	"internal/adapter":  {},
	"internal/core":     {"internal/adapter", "internal/pathx"},
	"internal/usage":    {"internal/adapter", "internal/platform"},
	"internal/app": {"internal/core", "internal/usage", "internal/adapter", "internal/adapter/claude",
		"internal/adapter/codex", "internal/adapter/opencodego", "internal/platform", "internal/pathx"},
	"internal/web":         {},
	"internal/web/demo":    {"internal/web"},
	"internal/web/live":    {"internal/web", "internal/app", "internal/core", "internal/adapter"},
	"internal/web/webtest": {"internal/web"},
}

// vendorAllowed is the allowance for internal/adapter/<vendor>.
var vendorAllowed = []string{"internal/adapter", "internal/platform"}

func rulesFor(rel string) (allow []string, restricted bool) {
	if a, ok := allowed[rel]; ok {
		return a, true
	}
	if strings.HasPrefix(rel, "internal/adapter/") {
		return vendorAllowed, true
	}
	return nil, false
}

// violations returns one message per forbidden import edge. imports maps a
// package import path to the import paths it imports.
func violations(module string, imports map[string][]string) []string {
	var out []string
	for pkg, deps := range imports {
		rel := strings.TrimPrefix(pkg, module+"/")
		allow, restricted := rulesFor(rel)
		for _, dep := range deps {
			inModule := dep == module || strings.HasPrefix(dep, module+"/")
			if !inModule {
				if first, _, _ := strings.Cut(dep, "/"); strings.Contains(first, ".") {
					out = append(out, pkg+" imports third-party package "+dep)
				}
				continue
			}
			if !restricted {
				continue
			}
			depRel := strings.TrimPrefix(dep, module+"/")
			ok := false
			for _, a := range allow {
				if depRel == a {
					ok = true
					break
				}
			}
			if !ok {
				out = append(out, pkg+" must not import "+dep)
			}
		}
	}
	return out
}

func TestViolationsDetectsForbiddenEdge(t *testing.T) {
	const m = "github.com/meatlover/modelpick"
	got := violations(m, map[string][]string{
		m + "/internal/core": {m + "/internal/usage"},
	})
	if len(got) != 1 || !strings.Contains(got[0], m+"/internal/core") || !strings.Contains(got[0], m+"/internal/usage") {
		t.Fatalf("want exactly 1 violation naming core and usage, got %q", got)
	}

	got = violations(m, map[string][]string{
		m + "/internal/pathx": {m + "/internal/adapter"},
		m + "/internal/core":  {m + "/internal/platform"},
	})
	if len(got) != 2 {
		t.Fatalf("want 2 violations (pathx->adapter, core->platform), got %q", got)
	}

	got = violations(m, map[string][]string{
		m + "/internal/adapter/claude": {"github.com/x/y"},
	})
	if len(got) != 1 {
		t.Fatalf("want exactly 1 violation for third-party import, got %q", got)
	}

	got = violations(m, map[string][]string{
		m + "/internal/app": {m + "/internal/web"},
	})
	if len(got) != 1 || !strings.Contains(got[0], m+"/internal/app") || !strings.Contains(got[0], m+"/internal/web") {
		t.Fatalf("want exactly 1 violation for app->web, got %q", got)
	}

	got = violations(m, map[string][]string{
		m + "/internal/web/live":    {m + "/internal/web", m + "/internal/app", m + "/internal/cli"},
		m + "/internal/web/webtest": {m + "/internal/web", m + "/internal/app"},
	})
	if len(got) != 2 {
		t.Fatalf("want 2 violations (live->cli, webtest->app), got %q", got)
	}

	got = violations(m, map[string][]string{
		m + "/internal/adapter/claude": {m + "/internal/adapter", m + "/internal/platform", "fmt"},
		m + "/internal/cli":            {m + "/internal/core", m + "/internal/usage"},
	})
	if len(got) != 0 {
		t.Fatalf("allowed edges flagged: %q", got)
	}
}

func TestModuleHasNoViolations(t *testing.T) {
	module, imports := moduleImports(t)
	if v := violations(module, imports); len(v) != 0 {
		t.Fatalf("layer violations:\n%s", strings.Join(v, "\n"))
	}
}

// moduleImports returns the module path and, per package, its imports, as
// `go list` reports them for the non-test build.
func moduleImports(t *testing.T) (module string, imports map[string][]string) {
	t.Helper()
	run := func(args ...string) string {
		cmd := exec.Command("go", args...)
		cmd.Dir = "../.."
		b, err := cmd.Output()
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				t.Fatalf("go %v: %v\n%s", args, err, ee.Stderr) // names an import cycle, if that is the cause
			}
			t.Fatalf("go %v: %v", args, err)
		}
		return strings.TrimSpace(string(b))
	}
	module = run("list", "-m")
	listing := run("list", "-f", "{{.ImportPath}}{{range .Imports}} {{.}}{{end}}", "./...")
	imports = map[string][]string{}
	for _, line := range strings.Split(listing, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		imports[f[0]] = f[1:]
	}
	if len(imports) == 0 {
		t.Fatal("go list returned no packages")
	}
	return module, imports
}
