package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/meatlover/modelpick/internal/app"
	"github.com/meatlover/modelpick/internal/app/apptest"
)

const wantDocs = "Docs: https://github.com/meatlover/modelpick/tree/main/docs\n"

// helpNames is every spelling a command is dispatched under.
var helpNames = []string{"pick", "env", "ls", "config", "conf", "ui", "doctor", "version", "--version", "help"}

func TestGeneralHelp(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		stdout, stderr, code := run(app.Deps{}, arg)
		if code != 0 || stderr != "" {
			t.Errorf("%s: exit %d, stderr %q; want 0 and empty", arg, code, stderr)
		}
		if !strings.HasPrefix(stdout, "usage:\n  modelpick pick [<profile>] [--refresh]\n") {
			t.Errorf("%s: stdout starts %q", arg, stdout)
		}
		if !strings.HasSuffix(stdout, wantDocs) {
			t.Errorf("%s: stdout does not end with the docs line:\n%s", arg, stdout)
		}
		for _, h := range commandHelp {
			if !strings.Contains(stdout, h.desc) {
				t.Errorf("%s: stdout lacks the description of %s: %q", arg, h.names[0], h.desc)
			}
		}
	}
}

func TestCommandHelp(t *testing.T) {
	for _, name := range helpNames {
		var want string
		for _, flag := range []string{"--help", "-h"} {
			stdout, stderr, code := run(app.Deps{}, name, flag)
			if code != 0 || stderr != "" {
				t.Errorf("%s %s: exit %d, stderr %q; want 0 and empty", name, flag, code, stderr)
			}
			first, _, _ := strings.Cut(stdout, "\n")
			if !strings.HasPrefix(first, "usage: modelpick ") {
				t.Errorf("%s %s: first line %q", name, flag, first)
			}
			if strings.HasPrefix(stdout, "usage:\n") {
				t.Errorf("%s %s printed the general usage", name, flag)
			}
			if want == "" {
				want = stdout
			} else if stdout != want {
				t.Errorf("%s: --help and -h differ", name)
			}
		}
		// `help <command>` is the same text.
		if name == "help" {
			continue
		}
		stdout, stderr, code := run(app.Deps{}, "help", name)
		if code != 0 || stderr != "" || stdout != want {
			t.Errorf("help %s: exit %d, stderr %q, stdout differs from %s --help", name, code, stderr, name)
		}
	}
}

func TestCommandHelpFirstLines(t *testing.T) {
	want := map[string]string{
		"pick":    "usage: modelpick pick [<profile>] [--refresh]",
		"env":     "usage: modelpick env <profile> [--refresh]",
		"ls":      "usage: modelpick ls [<account>…] [--refresh] [--json]",
		"config":  "usage: modelpick config",
		"ui":      "usage: modelpick ui [--port N] [--no-open]",
		"doctor":  "usage: modelpick doctor [all]",
		"version": "usage: modelpick version",
		"help":    "usage: modelpick help [<command>]",
	}
	for name, first := range want {
		stdout, _, _ := run(app.Deps{}, name, "--help")
		if got, _, _ := strings.Cut(stdout, "\n"); got != first {
			t.Errorf("%s --help first line %q, want %q", name, got, first)
		}
	}
}

// pick and ui parse their own arguments; --help must win, with no profile
// lookup, no config read and no flag-package message.
func TestHelpBeatsCommandParsing(t *testing.T) {
	d := apptest.NewDeps(t, testConfig)
	stdout, stderr, code := run(d, "pick", "--help")
	if code != 0 || stderr != "" || strings.Contains(stdout, `"status"`) {
		t.Errorf("pick --help: exit %d, stderr %q, stdout %q", code, stderr, stdout)
	}
	stdout, stderr, code = run(d, "pick", "two-claude", "-h")
	if code != 0 || stderr != "" || !strings.HasPrefix(stdout, "usage: modelpick pick") {
		t.Errorf("pick two-claude -h: exit %d, stderr %q, stdout %q", code, stderr, stdout)
	}
	stdout, stderr, code = run(d, "ui", "--help")
	if code != 0 || stderr != "" || strings.Contains(stdout, "flag:") || !strings.HasPrefix(stdout, "usage: modelpick ui") {
		t.Errorf("ui --help: exit %d, stderr %q, stdout %q", code, stderr, stdout)
	}
}

func TestHelpUnknownCommand(t *testing.T) {
	stdout, stderr, code := run(app.Deps{}, "help", "nosuch")
	if code != 1 || stdout != "" || !strings.Contains(stderr, `unknown command "nosuch"`) {
		t.Errorf("help nosuch: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	stdout, stderr, code = run(app.Deps{}, "help", "pick", "env")
	if code != 1 || stdout != "" || !strings.Contains(stderr, `unexpected argument "env"`) {
		t.Errorf("help pick env: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestBadUsageStillStderrExit1(t *testing.T) {
	for _, args := range [][]string{{}, {"frobnicate"}} {
		stdout, stderr, code := run(app.Deps{}, args...)
		if code != 1 || stdout != "" || !strings.Contains(stderr, "usage:\n  modelpick pick") {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
		}
	}
}

// dispatchCases reads the string literals of every case clause in dispatch.
func dispatchCases(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "cli.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "dispatch" {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			cc, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, e := range cc.List {
				if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					s, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatal(err)
					}
					out = append(out, s)
				}
			}
			return true
		})
	}
	return out
}

func TestHelpTableMatchesDispatch(t *testing.T) {
	cases := dispatchCases(t)
	if len(cases) < 8 {
		t.Fatalf("read only %v from dispatch; the AST walk is broken", cases)
	}
	var table []string
	for _, h := range commandHelp {
		table = append(table, h.names...)
	}
	sort.Strings(cases)
	sort.Strings(table)
	if strings.Join(cases, " ") != strings.Join(table, " ") {
		t.Errorf("dispatch cases %v differ from help table names %v", cases, table)
	}
}

func TestMissingConfigHint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent", "config.json")
	d := apptest.NewDeps(t, testConfig)
	d.Getenv = func(k string) string {
		if k == "MODELPICK_CONFIG" {
			return path
		}
		return ""
	}
	hint := "no config file at " + path + ". Start from the example:\n" +
		"https://github.com/meatlover/modelpick/blob/main/examples/config.json\n" +
		"(also in each release archive as config.example.json), then run `modelpick doctor`."
	flat := strings.ReplaceAll(hint, "\n", " ")
	for _, args := range [][]string{{"pick"}, {"ls"}, {"config"}} {
		stdout, stderr, code := run(d, args...)
		if code != 1 || !strings.Contains(stderr, "modelpick: "+flat+"\n") {
			t.Errorf("%v: exit %d, stderr %q; stdout %q", args, code, stderr, stdout)
		}
	}
	for _, args := range [][]string{{"pick", "p"}, {"env", "p"}} {
		stdout, _, code := run(d, args...)
		if code != 1 || !strings.Contains(stdout, flat) {
			t.Errorf("%v: exit %d, stdout %q", args, code, stdout)
		}
	}
	stdout, _, code := run(d, "doctor")
	if code != 1 || !strings.Contains(stdout, "ERROR  config: "+flat) {
		t.Errorf("doctor: exit %d, stdout %q", code, stdout)
	}
}

// A config that exists but cannot be read is not "missing": the old text stays.
func TestUnreadableConfigKeepsOldError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(testConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if f, err := os.Open(path); err == nil {
		_ = f.Close()
		t.Skip("file stays readable (running as root?)")
	}
	d := apptest.NewDeps(t, testConfig)
	d.Getenv = func(k string) string {
		if k == "MODELPICK_CONFIG" {
			return path
		}
		return ""
	}
	for _, args := range [][]string{{"ls"}, {"config"}} {
		_, stderr, code := run(d, args...)
		if code != 1 || !strings.Contains(stderr, "read config: ") || !strings.Contains(stderr, "permission denied") ||
			strings.Contains(stderr, "no config file") {
			t.Errorf("%v: exit %d, stderr %q", args, code, stderr)
		}
	}
}
