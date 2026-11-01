package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// A child process that inherits modelpick's working directory fails when
// that directory has been deleted under a long-running `modelpick ui`
// (opencode refuses to start). Every function that builds an exec.Cmd must
// therefore set its Dir (platform.ChildDir, or a directory of its own).
//
// The check is best-effort: any .Dir assignment in the same function counts,
// and it does not see an &exec.Cmd{} literal, an aliased os/exec import or
// os.StartProcess. None of those occurs today; tighten the check (match the
// assigned variable to the command) if one is added.

// execCallsWithoutDir lists, for the Go source in src, each exec.Command or
// exec.CommandContext call whose enclosing function never assigns a .Dir
// field.
func execCallsWithoutDir(fset *token.FileSet, name string, src any) ([]string, error) {
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		var calls []token.Pos
		setsDir := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				if sel, ok := n.Fun.(*ast.SelectorExpr); ok {
					if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "exec" &&
						(sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext") {
						calls = append(calls, n.Pos())
					}
				}
			case *ast.AssignStmt:
				for _, lhs := range n.Lhs {
					if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "Dir" {
						setsDir = true
					}
				}
			}
			return true
		})
		if !setsDir {
			for _, p := range calls {
				out = append(out, fset.Position(p).String())
			}
		}
	}
	return out, nil
}

func TestExecCommandsSetDir(t *testing.T) {
	fset := token.NewFileSet()
	var bad []string
	for _, root := range []string{"..", "../../cmd"} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			found, err := execCallsWithoutDir(fset, p, nil)
			bad = append(bad, found...)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, b := range bad {
		t.Errorf("%s: exec.Cmd built without setting Dir; use platform.ChildDir()", b)
	}
}

// The check itself rejects a function that builds a command without Dir and
// accepts one that sets it.
func TestExecCallsWithoutDirDetects(t *testing.T) {
	src := `package p
import "os/exec"
func bad() { _ = exec.Command("x").Run() }
func good() { c := exec.Command("x"); c.Dir = "/"; _ = c.Run() }
`
	got, err := execCallsWithoutDir(token.NewFileSet(), "p.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.HasPrefix(got[0], "p.go:3:") {
		t.Fatalf("got %v, want one finding in bad() on line 3", got)
	}
}
