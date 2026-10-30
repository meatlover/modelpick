package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/meatlover/modelpick/internal/adapter"
	"github.com/meatlover/modelpick/internal/core"
)

// jsonBlock matches a fenced json block whose body is a config document, i.e.
// starts with `{` and then a "version" key (on the same or the next line).
var jsonBlock = regexp.MustCompile("(?s)```json\n(\\{\\s*\"version\".*?)\n```")

func TestDocsExampleMatchesFile(t *testing.T) {
	doc, err := os.ReadFile("../../docs/configuration.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks := jsonBlock.FindAllStringSubmatch(string(doc), -1)
	if len(blocks) == 0 {
		t.Fatal("docs/configuration.md has no ```json config block")
	}
	for i, b := range blocks {
		if _, err := core.Parse([]byte(b[1]), "docs/configuration.md", "/home/u"); err != nil {
			t.Errorf("config block %d does not parse: %v", i+1, err)
		}
	}
	// The running example must be the shipped example, byte for byte.
	example, err := os.ReadFile("../../examples/config.json")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSpace(string(example))
	found := false
	for _, b := range blocks {
		found = found || strings.TrimSpace(b[1]) == want
	}
	if !found {
		t.Error("no config block in docs/configuration.md equals examples/config.json")
	}
}

func readDoc(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// docSection returns the lines of a markdown file after the line heading,
// up to the next heading of the same or a higher level.
func docSection(t *testing.T, path, heading string) string {
	t.Helper()
	level := strings.Index(heading, " ")
	var out []string
	in := false
	for _, line := range strings.Split(readDoc(t, path), "\n") {
		if hashes := len(line) - len(strings.TrimLeft(line, "#")); in && hashes > 0 && hashes <= level &&
			strings.HasPrefix(line[hashes:], " ") {
			break
		}
		if in {
			out = append(out, line)
		}
		in = in || line == heading
	}
	if !in {
		t.Fatalf("%s has no heading %q", path, heading)
	}
	return strings.Join(out, "\n")
}

const referenceDoc = "../../docs/reference.md"

var commandHeading = regexp.MustCompile("(?m)^### `modelpick ([^` ]+)`$")

// TestReferenceCoversCommands: every command of the help table has a section
// in docs/reference.md holding its usage lines and aliases, and every command
// section names a real command.
func TestReferenceCoversCommands(t *testing.T) {
	commands := docSection(t, referenceDoc, "## Commands")
	headings := map[string]bool{}
	for _, m := range commandHeading.FindAllStringSubmatch(commands, -1) {
		headings[m[1]] = true
		if h, ok := helpFor(m[1]); !ok || h.names[0] != m[1] {
			t.Errorf("docs/reference.md has a section for %q, which is not a command's canonical name", m[1])
		}
	}
	for _, h := range commandHelp {
		name := h.names[0]
		if !headings[name] {
			t.Errorf("command %q has no `### `modelpick %s`` section in docs/reference.md", name, name)
			continue
		}
		section := docSection(t, referenceDoc, "### `modelpick "+name+"`")
		for _, u := range h.usage {
			if !strings.Contains(section, "modelpick "+u+"\n") {
				t.Errorf("docs/reference.md, %s: usage line %q is missing", name, "modelpick "+u)
			}
		}
		for _, alias := range h.names[1:] {
			if !strings.Contains(section, "`"+alias+"`") {
				t.Errorf("docs/reference.md, %s: alias %q is not named", name, alias)
			}
		}
	}
}

// jsonFields returns the json names of v's struct fields, in order.
func jsonFields(v any) []string {
	var out []string
	rt := reflect.TypeOf(v)
	for i := 0; i < rt.NumField(); i++ {
		name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			out = append(out, name)
		}
	}
	return out
}

// statusValues reads the Status constants from internal/core/result.go, so a
// new status cannot be missed by a list kept here.
func statusValues(t *testing.T) []core.Status {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "../core/result.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []core.Status
	ast.Inspect(f, func(n ast.Node) bool {
		if vs, ok := n.(*ast.ValueSpec); ok && vs.Type != nil && typeName(vs.Type) == "Status" {
			for _, v := range vs.Values {
				s, _ := strconv.Unquote(v.(*ast.BasicLit).Value)
				out = append(out, core.Status(s))
			}
		}
		return true
	})
	if len(out) < 3 {
		t.Fatalf("found %d Status constants in result.go", len(out))
	}
	return out
}

var tableKey = regexp.MustCompile("(?m)^\\| `([a-z_]+)` \\|")

// TestReferenceCoversPickJSON: the field table of docs/reference.md lists the
// result's fields in output order, every nested field is named, and the
// status table gives each status its real exit code.
func TestReferenceCoversPickJSON(t *testing.T) {
	section := docSection(t, referenceDoc, "## `pick` JSON")
	var rows []string
	for _, m := range tableKey.FindAllStringSubmatch(section, -1) {
		rows = append(rows, m[1])
	}
	fields := jsonFields(core.Result{})
	if got := strings.Join(rows[:min(len(rows), len(fields))], ","); got != strings.Join(fields, ",") {
		t.Errorf("docs/reference.md field table: %s; the result's fields are %s", got, strings.Join(fields, ","))
	}
	ref := readDoc(t, referenceDoc)
	for _, v := range []any{core.Skip{}, core.RunError{}, core.LBPick{}, adapter.Account{}} {
		for _, f := range jsonFields(v) {
			if !strings.Contains(ref, `"`+f+`"`) && !strings.Contains(ref, "`"+f+"`") {
				t.Errorf("docs/reference.md does not name the %T field %q", v, f)
			}
		}
	}
	statuses := docSection(t, referenceDoc, "### Status values")
	for _, s := range statusValues(t) {
		want := "| `" + string(s) + "` | " + strconv.Itoa(core.Result{Status: s}.ExitCode()) + " |"
		if !strings.Contains(statuses, want) {
			t.Errorf("docs/reference.md status table has no row starting %q", want)
		}
	}
}

// resultBlock matches a fenced json block holding a pick result.
var resultBlock = regexp.MustCompile("(?s)```json\n(\\{\n  \"status\".*?)\n```")

// TestReadmeSampleIsTheWorkedExample: the README's sample pick is the worked
// example of docs/concepts.md, whose result a core test checks.
func TestReadmeSampleIsTheWorkedExample(t *testing.T) {
	readme := resultBlock.FindStringSubmatch(readDoc(t, "../../README.md"))
	concepts := resultBlock.FindStringSubmatch(docSection(t, "../../docs/concepts.md", "## A worked example"))
	if readme == nil || concepts == nil {
		t.Fatal("README.md or docs/concepts.md has no pick result block")
	}
	if readme[1] != concepts[1] {
		t.Errorf("README.md's sample pick differs from the worked example in docs/concepts.md")
	}
}

// userDocs are the documents a reader of the repository opens.
func userDocs(t *testing.T) []string {
	t.Helper()
	docs, err := filepath.Glob("../../docs/*.md")
	if err != nil || len(docs) < 7 {
		t.Fatalf("docs/*.md: %v %v", docs, err)
	}
	return append([]string{"../../README.md", "../../CONTRIBUTING.md", "../../SECURITY.md"}, docs...)
}
