package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Line limits per function, method or function literal, counted from the
// `func` keyword to the closing brace. Go has no classes; a type's methods
// are each limited. Tests get more room for table literals.
const (
	maxFuncLines     = 60
	maxTestFuncLines = 80
)

// longFuncs returns one "file:line name: N lines (limit L)" entry per
// function in src that is over its limit.
func longFuncs(fset *token.FileSet, path string, src any) ([]string, error) {
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, err
	}
	limit := maxFuncLines
	if strings.HasSuffix(path, "_test.go") {
		limit = maxTestFuncLines
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		var body *ast.BlockStmt
		name := "func literal"
		switch fn := n.(type) {
		case *ast.FuncDecl:
			body, name = fn.Body, fn.Name.Name
			if fn.Recv != nil && len(fn.Recv.List) == 1 {
				name = recvName(fn.Recv.List[0].Type) + "." + name
			}
		case *ast.FuncLit:
			body = fn.Body
		}
		if body == nil {
			return true
		}
		start := fset.Position(n.Pos())
		lines := fset.Position(body.Rbrace).Line - start.Line + 1
		if lines > limit {
			out = append(out, fmt.Sprintf("%s:%d %s: %d lines (limit %d)", path, start.Line, name, lines, limit))
		}
		return true
	})
	return out, nil
}

func recvName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvName(t.X)
	case *ast.IndexExpr:
		return recvName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return "?"
}

func TestLongFuncsDetectsOverLimit(t *testing.T) {
	body := strings.Repeat("\tx++\n", maxFuncLines-2) // func line + body + } = limit
	ok := "package p\nfunc ok() {\n" + body + "}\n"
	over := "package p\ntype T struct{}\nfunc (t *T) over() {\n" + body + "\tx++\n}\n"
	fset := token.NewFileSet()
	if got, err := longFuncs(fset, "ok.go", ok); err != nil || len(got) != 0 {
		t.Fatalf("function at the limit flagged: %q, %v", got, err)
	}
	got, err := longFuncs(fset, "over.go", over)
	if err != nil || len(got) != 1 || !strings.Contains(got[0], "T.over: 61 lines") {
		t.Fatalf("want one T.over violation of 61 lines, got %q, %v", got, err)
	}
	if got, _ := longFuncs(fset, "over_test.go", over); len(got) != 0 {
		t.Fatalf("test file uses the test limit %d, got %q", maxTestFuncLines, got)
	}
}

// TestFunctionLengths walks every .go file in the module, tests included.
func TestFunctionLengths(t *testing.T) {
	root := "../.."
	fset := token.NewFileSet()
	var over []string
	files := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && p != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		files++
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		v, err := longFuncs(fset, rel, src)
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
		t.Fatal("no .go files found")
	}
	sort.Strings(over)
	if len(over) > 0 {
		t.Fatalf("%d function(s) over the line limit; split them:\n%s", len(over), strings.Join(over, "\n"))
	}
}
