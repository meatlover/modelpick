package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// versionLiteral matches a string that spells a modelpick version.
var versionLiteral = regexp.MustCompile(`modelpick/[0-9]`)

// versionLiterals returns one "file:line literal" entry per string literal
// in src that matches versionLiteral.
func versionLiterals(fset *token.FileSet, path string, src any) ([]string, error) {
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		return nil, err
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			s = lit.Value
		}
		if versionLiteral.MatchString(s) {
			out = append(out, fset.Position(lit.Pos()).String()+" "+lit.Value)
		}
		return true
	})
	return out, nil
}

// TestVersionHasOneHome fails on any non-test Go file that spells out a
// "modelpick/<digit>" User-Agent. internal/cli.Version is the only home of
// the version; app.DefaultDeps builds the User-Agent from it.
func TestVersionHasOneHome(t *testing.T) {
	root := "../.."
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			if rel != "." && strings.HasPrefix(d.Name(), ".") || rel == "e2e/node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		hits, perr := versionLiterals(token.NewFileSet(), path, nil)
		if perr != nil {
			return perr
		}
		found = append(found, hits...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range found {
		t.Errorf("version literal outside internal/cli/version.go: %s", h)
	}
}
