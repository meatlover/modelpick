package archtest

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// repeatLines returns n copies of "  x();\n".
func repeatLines(n int) string { return strings.Repeat("  x();\n", n) }

func TestLongJSBlocksCountsArrowAndFunction(t *testing.T) {
	// 1 opening line + 59 body + 1 closing = 60 lines: at the limit.
	ok := "function f() {\n" + repeatLines(58) + "}\n"
	if got, err := longJSBlocks("ok.js", []byte(ok)); err != nil || len(got) != 0 {
		t.Fatalf("60-line block flagged: %q, %v", got, err)
	}
	fn := "function f() {\n" + repeatLines(59) + "}\n"
	arrow := "const g = () => {\n" + repeatLines(59) + "};\n"
	src := fn + arrow
	got, err := longJSBlocks("a.js", []byte(src))
	if err != nil || len(got) != 2 {
		t.Fatalf("want 2 entries, got %q, %v", got, err)
	}
	if got[0] != "a.js:1: 61 lines (limit 60)" {
		t.Fatalf("function entry: %q", got[0])
	}
	if got[1] != "a.js:62: 61 lines (limit 60)" {
		t.Fatalf("arrow entry: %q", got[1])
	}
}

func TestLongJSBlocksSkipsStringsAndComments(t *testing.T) {
	noise := "  var a = '{';\n" +
		"  var b = \"}}\";\n" +
		"  var c = `{ ${ {a: 1}.a } }`;\n" +
		"  // {\n" +
		"  /* { */\n" +
		"  /* }\n  } */\n" +
		"  var d = 'it\\'s {';\n"
	// noise is 8 lines; 1 + 8 + 51 + 1 = 61.
	src := "function f() {\n" + noise + repeatLines(51) + "}\n"
	got, err := longJSBlocks("n.js", []byte(src))
	if err != nil || len(got) != 1 || got[0] != "n.js:1: 61 lines (limit 60)" {
		t.Fatalf("want one 61-line entry, got %q, %v", got, err)
	}
}

func TestLongJSBlocksUnbalanced(t *testing.T) {
	_, err := longJSBlocks("bad.js", []byte("function f() {\n"))
	if err == nil || !strings.Contains(err.Error(), "bad.js") || !strings.Contains(err.Error(), "1") {
		t.Fatalf("want error naming bad.js and line 1, got %v", err)
	}
	if _, err := longJSBlocks("bad2.js", []byte("x();\n}\n")); err == nil || !strings.Contains(err.Error(), "bad2.js:2") {
		t.Fatalf("stray close: got %v", err)
	}
}

func TestLongJSBlocksIgnoresObjectLiterals(t *testing.T) {
	src := "const o = {\n" + repeatLines(70) + "};\n"
	if got, err := longJSBlocks("o.js", []byte(src)); err != nil || len(got) != 0 {
		t.Fatalf("object literal measured: %q, %v", got, err)
	}
}

// TestUIJavaScriptFunctionLength walks the page's scripts and the browser
// tests' (e2e/tests; e2e/node_modules is not ours). Each root must hold a
// script, so a moved directory fails here instead of passing unmeasured.
func TestUIJavaScriptFunctionLength(t *testing.T) {
	var over []string
	for _, rel := range []string{"internal/web/static", "e2e/tests"} {
		over = append(over, longJSBlocksUnder(t, rel)...)
	}
	sort.Strings(over)
	if len(over) > 0 {
		t.Fatalf("%d JS block(s) over the line limit; split them:\n%s", len(over), strings.Join(over, "\n"))
	}
}

// longJSBlocksUnder is longJSBlocks over every .js file under rel (relative
// to the module root), named by its module-relative path.
func longJSBlocksUnder(t *testing.T, rel string) []string {
	t.Helper()
	root := filepath.Join("../..", rel)
	var over []string
	files := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".js") {
			return nil
		}
		files++
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(filepath.Join(rel, strings.TrimPrefix(p, root)))
		v, err := longJSBlocks(name, src)
		if err != nil {
			return err
		}
		over = append(over, v...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatalf("%s holds no .js file: the length rule measured nothing there", rel)
	}
	return over
}

const maxJSBlockLines = 60

// jsFrame is one open `{`: where it opened, whether it is a measured block
// (a function or arrow body) and whether it is a template `${`.
type jsFrame struct {
	line    int
	measure bool
	tmpl    bool
}

// jsScan is a single forward scan over a script. It does not know regex
// literals: one containing a quote or brace unbalances the scan and fails
// loudly. Removable by a real tokenizer, deferred because the page writes
// such patterns with new RegExp("...").
type jsScan struct {
	path  string
	src   []byte
	i     int
	line  int
	last  string // previous significant token: ")", "=>" or "x"
	stack []jsFrame
	out   []string
}

// longJSBlocks returns one "path:line: N lines (limit 60)" entry per function
// or arrow body (a `{` right after `)` or `=>`) over the limit. It returns an
// error naming the file and line of an unbalanced brace.
func longJSBlocks(path string, src []byte) ([]string, error) {
	s := &jsScan{path: path, src: src, line: 1, last: "x"}
	for s.i < len(s.src) {
		if err := s.step(); err != nil {
			return nil, err
		}
	}
	if n := len(s.stack); n > 0 {
		return nil, fmt.Errorf("%s:%d: unmatched '{'", path, s.stack[n-1].line)
	}
	return s.out, nil
}

func (s *jsScan) peek(off int) byte {
	if s.i+off < len(s.src) {
		return s.src[s.i+off]
	}
	return 0
}

func (s *jsScan) step() error {
	c := s.src[s.i]
	switch {
	case c == '\n':
		s.line++
		s.i++
	case c == ' ' || c == '\t' || c == '\r':
		s.i++
	case c == '/' && s.peek(1) == '/':
		for s.i < len(s.src) && s.src[s.i] != '\n' {
			s.i++
		}
	case c == '/' && s.peek(1) == '*':
		s.blockComment()
	case c == '\'' || c == '"':
		s.quoted(c)
	case c == '`':
		s.i++
		s.template()
	case c == '{':
		s.stack = append(s.stack, jsFrame{line: s.line, measure: s.last == ")" || s.last == "=>"})
		s.last = "x"
		s.i++
	case c == '}':
		return s.closeBrace()
	default:
		s.last = "x"
		if c == ')' {
			s.last = ")"
		} else if c == '>' && s.i > 0 && s.src[s.i-1] == '=' {
			s.last = "=>"
		}
		s.i++
	}
	return nil
}

func (s *jsScan) blockComment() {
	s.i += 2
	for s.i < len(s.src) && !(s.src[s.i] == '*' && s.peek(1) == '/') {
		if s.src[s.i] == '\n' {
			s.line++
		}
		s.i++
	}
	s.i += 2
}

// quoted skips a '...' or "..." string; an escape skips the next byte.
func (s *jsScan) quoted(q byte) {
	s.i++
	for s.i < len(s.src) && s.src[s.i] != q && s.src[s.i] != '\n' {
		if s.src[s.i] == '\\' {
			s.i++
		}
		s.i++
	}
	s.i++
	s.last = "x"
}

// template skips template text up to the closing backtick, or up to a `${`,
// which opens a code frame that closeBrace resumes the template from.
func (s *jsScan) template() {
	for s.i < len(s.src) {
		c := s.src[s.i]
		switch {
		case c == '`':
			s.i++
			s.last = "x"
			return
		case c == '\\':
			s.i++
		case c == '$' && s.peek(1) == '{':
			s.stack = append(s.stack, jsFrame{line: s.line, tmpl: true})
			s.last = "x"
			s.i += 2
			return
		}
		if s.i < len(s.src) && s.src[s.i] == '\n' {
			s.line++
		}
		s.i++
	}
}

func (s *jsScan) closeBrace() error {
	n := len(s.stack)
	if n == 0 {
		return fmt.Errorf("%s:%d: unmatched '}'", s.path, s.line)
	}
	f := s.stack[n-1]
	s.stack = s.stack[:n-1]
	s.i++
	s.last = "x"
	if f.tmpl {
		s.template()
		return nil
	}
	if f.measure {
		if lines := s.line - f.line + 1; lines > maxJSBlockLines {
			s.out = append(s.out, fmt.Sprintf("%s:%d: %d lines (limit %d)", s.path, f.line, lines, maxJSBlockLines))
		}
	}
	return nil
}
