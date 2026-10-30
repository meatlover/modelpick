package cli

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The doctor messages test reads the Go source, not a list kept by hand, so
// a new message fails it until the troubleshooting table has a row.
//
// What counts as a message: the last argument of every call to a function
// named add (findings.add, the adapters' add closures, never.go's add
// parameter), the Message of every Finding literal, and every value appended
// to a Reading's Notes, in internal/app and internal/adapter; plus every
// fmt.Sprintf in core.EffectiveCaps, whose warnings doctor passes on.
//
// How a message is matched: its text is reduced to the literal fragments
// between the values filled in at run time (format verbs, variables, calls).
// A row of the table matches when its Message cell holds every fragment of
// at least 3 characters, in order. The doc writes the values as <name>, so
// `login expires <time>; log in again in Claude Code ...` matches the
// template "login expires "+at+"; log in again in "+tool+" with this ...".
//
// A message that is a value passed in from elsewhere, with no literal of its
// own, must be classified in passedOn, saying where its text comes from.

// passedOn lists the messages with no literal text at their site, by
// "file: expression".
var passedOn = map[string]string{
	"doctor.go: err.Error()": "a config error (row `config <path>: <field>: <problem>`; the rules are in " +
		"configuration.md) or AdapterFor's `no adapter for vendor`, which a parsed config cannot reach",
	"doctor.go: x.Message": "an adapter Check finding, scanned at the adapters' add calls",
	"doctor.go: note":      "a Reading.Notes entry, scanned at append(r.Notes, …) in the adapters",
	"doctor.go: w":         "a core.EffectiveCaps warning, scanned in internal/core/windows.go",
}

// msgTemplate is one message found in the source.
type msgTemplate struct {
	where     string   // file:line
	fragments []string // literal text between run-time values
}

// hole stands for a value filled in at run time.
const hole = "\x00"

var verb = regexp.MustCompile(`%[-+# 0-9.*]*[a-zA-Z]`)

// scanFile collects the doctor messages of one Go file.
type scanFile struct {
	t      *testing.T
	fset   *token.FileSet
	name   string
	consts map[string]string // the package's string constants
	found  *[]msgTemplate
}

func (s scanFile) pos(n ast.Node) string {
	p := s.fset.Position(n.Pos())
	return filepath.Base(p.Filename) + ":" + strconv.Itoa(p.Line)
}

// formats renders e as format strings with hole for every run-time value.
// fn is the enclosing function declaration, for tracing local variables.
func (s scanFile) formats(e ast.Expr, fn *ast.FuncDecl) []string {
	switch e := e.(type) {
	case *ast.BasicLit:
		if v, err := strconv.Unquote(e.Value); err == nil && e.Kind == token.STRING {
			return []string{v}
		}
	case *ast.ParenExpr:
		return s.formats(e.X, fn)
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			var out []string
			for _, l := range s.formats(e.X, fn) {
				for _, r := range s.formats(e.Y, fn) {
					out = append(out, l+r)
				}
			}
			return out
		}
	case *ast.CallExpr:
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok && len(e.Args) > 0 {
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == "fmt" && (sel.Sel.Name == "Sprintf" || sel.Sel.Name == "Errorf") {
				var out []string
				for _, f := range s.formats(e.Args[0], fn) {
					f = strings.ReplaceAll(f, "%%", "\x01")
					out = append(out, strings.ReplaceAll(verb.ReplaceAllString(f, hole), "\x01", "%"))
				}
				return out
			}
		}
	case *ast.Ident:
		if v, ok := s.consts[e.Name]; ok {
			return []string{v}
		}
		if vals := localValues(e.Name, fn); len(vals) > 0 {
			var out []string
			for _, v := range vals {
				out = append(out, s.formats(v, fn)...)
			}
			return out
		}
	}
	return []string{hole}
}

// localValues is every value fn assigns to the variable name: plain and
// tuple assignments, and the keys of a range over a composite literal.
func localValues(name string, fn *ast.FuncDecl) []ast.Expr {
	var out []ast.Expr
	if fn == nil || fn.Body == nil {
		return nil
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for i, l := range n.Lhs {
				if id, ok := l.(*ast.Ident); ok && id.Name == name && len(n.Rhs) == len(n.Lhs) {
					out = append(out, n.Rhs[i])
				}
			}
		case *ast.RangeStmt:
			lit, ok := n.X.(*ast.CompositeLit)
			if key, isID := n.Key.(*ast.Ident); ok && isID && key.Name == name {
				for _, el := range lit.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok {
						out = append(out, kv.Key)
					}
				}
			}
		}
		return true
	})
	return out
}

// fragments splits a format at its holes, keeping pieces of 3 or more
// characters after trimming.
func fragments(format string) []string {
	var out []string
	for _, p := range strings.Split(format, hole) {
		if p = strings.TrimSpace(p); len(p) >= 3 {
			out = append(out, p)
		}
	}
	return out
}

// record adds the message expression e found at n.
func (s scanFile) record(n ast.Node, e ast.Expr, fn *ast.FuncDecl, params map[string]bool) {
	if id, ok := e.(*ast.Ident); ok && params[id.Name] {
		return // a parameter: its callers are scanned
	}
	for _, f := range s.formats(e, fn) {
		frags := fragments(f)
		if len(frags) > 0 {
			*s.found = append(*s.found, msgTemplate{s.pos(n), frags})
			continue
		}
		key := s.name + ": " + exprText(s.fset, e)
		if _, ok := passedOn[key]; !ok {
			s.t.Errorf("%s: message %q has no literal text; classify it in passedOn", s.pos(n), key)
		}
	}
}

func exprText(fset *token.FileSet, e ast.Expr) string {
	var b strings.Builder
	_ = printer.Fprint(&b, fset, e)
	return b.String()
}

// walk visits n, tracking the enclosing declaration and the parameters of
// every enclosing function, and records each message site.
func (s scanFile) walk(n ast.Node, fn *ast.FuncDecl, params map[string]bool) {
	ast.Inspect(n, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			s.walk(n.Body, fn, withParams(params, n.Type))
			return false
		case *ast.CallExpr:
			s.call(n, fn, params)
		case *ast.CompositeLit:
			if typeName(n.Type) == "Finding" {
				for _, el := range n.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok && exprText(s.fset, kv.Key) == "Message" {
						s.record(n, kv.Value, fn, params)
					}
				}
			}
		}
		return true
	})
}

// call records the message of an add(…, msg) call and the values of an
// append(x.Notes, …).
func (s scanFile) call(n *ast.CallExpr, fn *ast.FuncDecl, params map[string]bool) {
	switch name := typeName(n.Fun); {
	case name == "add" && len(n.Args) >= 2:
		s.record(n, n.Args[len(n.Args)-1], fn, params)
	case name == "append" && len(n.Args) >= 2 && strings.HasSuffix(exprText(s.fset, n.Args[0]), ".Notes"):
		for _, a := range n.Args[1:] {
			s.record(n, a, fn, params)
		}
	}
}

// typeName is the last name of an identifier or selector: Finding for
// adapter.Finding, add for f.add.
func typeName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	}
	return ""
}

func withParams(outer map[string]bool, ft *ast.FuncType) map[string]bool {
	m := map[string]bool{}
	for k := range outer {
		m[k] = true
	}
	for _, f := range ft.Params.List {
		for _, n := range f.Names {
			m[n.Name] = true
		}
	}
	return m
}

// packageFiles parses the non-test Go files of dir.
func packageFiles(t *testing.T, fset *token.FileSet, dir string) []*ast.File {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no Go files in %s: %v", dir, err)
	}
	var files []*ast.File
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	return files
}

// stringConsts maps each string constant of files to its value.
func stringConsts(files []*ast.File) map[string]string {
	m := map[string]string{}
	for _, f := range files {
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				continue
			}
			for _, spec := range g.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, n := range vs.Names {
					if i < len(vs.Values) {
						if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							m[n.Name], _ = strconv.Unquote(lit.Value)
						}
					}
				}
			}
		}
	}
	return m
}

// doctorMessages scans the doctor's message sites (see the comment at the
// top of this file).
func doctorMessages(t *testing.T) []msgTemplate {
	t.Helper()
	var found []msgTemplate
	fset := token.NewFileSet()
	dirs := []string{"../app", "../adapter", "../adapter/claude", "../adapter/codex", "../adapter/opencodego"}
	for _, dir := range dirs {
		files := packageFiles(t, fset, dir)
		consts := stringConsts(files)
		for _, f := range files {
			s := scanFile{t, fset, filepath.Base(fset.Position(f.Pos()).Filename), consts, &found}
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok {
					s.walk(fd.Body, fd, withParams(nil, fd.Type))
				}
			}
		}
	}
	for _, f := range packageFiles(t, fset, "../core") {
		s := scanFile{t, fset, "windows.go", nil, &found}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name.Name != "EffectiveCaps" {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok && exprText(fset, c.Fun) == "fmt.Sprintf" {
					s.record(c, c, fd, nil)
				}
				return true
			})
		}
	}
	return found
}

// troubleshootingRows returns the Message cell of every row of the
// troubleshooting table in docs/operations.md.
func troubleshootingRows(t *testing.T) []string {
	t.Helper()
	section := docSection(t, "../../docs/operations.md", "## Troubleshooting")
	var rows []string
	for _, line := range strings.Split(section, "\n") {
		cells := strings.Split(line, " | ")
		if strings.HasPrefix(line, "| `") && len(cells) >= 3 {
			rows = append(rows, strings.TrimPrefix(cells[0], "| "))
		}
	}
	if len(rows) < 10 {
		t.Fatalf("the troubleshooting table has %d message rows", len(rows))
	}
	return rows
}

func rowMatches(row string, frags []string) bool {
	at := 0
	for _, f := range frags {
		i := strings.Index(row[at:], f)
		if i < 0 {
			return false
		}
		at += i + len(f)
	}
	return true
}

// TestTroubleshootingCoversDoctorMessages: every message doctor can print has
// a row in the troubleshooting table of docs/operations.md.
func TestTroubleshootingCoversDoctorMessages(t *testing.T) {
	found := doctorMessages(t)
	rows := troubleshootingRows(t)
	// Known messages, from each kind of site, so a scan that reads nothing
	// cannot pass: an add call with a Sprintf, a constant, a traced local,
	// a range key, a note, and core's cap warning.
	for _, want := range []string{"never picked, choice", "the next usage call asks Codex to renew it",
		"; renewal fails until you log in again in", "cannot update stored usage:",
		"additional_rate_limits present", "; ignored the last"} {
		seen := false
		for _, m := range found {
			seen = seen || strings.Contains(strings.Join(m.fragments, hole), want)
		}
		if !seen {
			t.Errorf("the scan did not find the message containing %q", want)
		}
	}
	for _, m := range found {
		matched := false
		for _, r := range rows {
			matched = matched || rowMatches(r, m.fragments)
		}
		if !matched {
			t.Errorf("%s: doctor message with the text %q has no row in the troubleshooting table of docs/operations.md",
				m.where, strings.Join(m.fragments, " … "))
		}
	}
}
