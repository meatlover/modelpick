package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

var (
	fence    = regexp.MustCompile("(?ms)^```.*?^```")
	codeSpan = regexp.MustCompile("`[^`\n]*`")
	mdLink   = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)\)`)
	heading  = regexp.MustCompile(`(?m)^#{1,6} (.+)$`)
)

// prose is a markdown file without its fenced blocks, so a heading or link
// inside a code example does not count.
func prose(text string) string { return fence.ReplaceAllString(text, "") }

// anchors returns the GitHub anchors of a markdown file's headings: lower
// case, every character but letters, digits, spaces, '-' and '_' dropped,
// spaces turned into '-', and -1, -2, … added to repeats.
func anchors(text string) map[string]bool {
	out := map[string]bool{}
	seen := map[string]int{}
	for _, m := range heading.FindAllStringSubmatch(prose(text), -1) {
		var b strings.Builder
		for _, r := range strings.ToLower(strings.TrimSpace(m[1])) {
			switch {
			case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_':
				b.WriteRune(r)
			case r == ' ':
				b.WriteRune('-')
			}
		}
		a := b.String()
		if n := seen[a]; n > 0 {
			seen[a]++
			a += "-" + strconv.Itoa(n)
		} else {
			seen[a] = 1
		}
		out[a] = true
	}
	return out
}

// TestDocLinksResolve: every relative link in the user docs names a file or
// directory that exists, and every #anchor a heading of its target.
func TestDocLinksResolve(t *testing.T) {
	checked := 0
	for _, doc := range userDocs(t) {
		text := readDoc(t, doc)
		body := codeSpan.ReplaceAllString(prose(text), "")
		for _, m := range mdLink.FindAllStringSubmatch(body, -1) {
			target := m[1]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			checked++
			path, anchor, _ := strings.Cut(target, "#")
			file := doc
			if path != "" {
				file = filepath.Join(filepath.Dir(doc), path)
				if _, err := os.Stat(file); err != nil {
					t.Errorf("%s: link %q: %v", filepath.Base(doc), target, err)
					continue
				}
			}
			if anchor != "" && !anchors(readDoc(t, file))[anchor] {
				t.Errorf("%s: link %q: %s has no heading with that anchor", filepath.Base(doc), target, filepath.Base(file))
			}
		}
	}
	if checked < 30 {
		t.Errorf("checked only %d relative links; the link pattern may have stopped matching", checked)
	}
}

var (
	versionPin  = regexp.MustCompile(`@v0\.|checkout v0\.|download v0\.`)
	privateText = regexp.MustCompile(`(?i)GOPRIVATE|repository is private|until the repository is public`)
)

// TestDocsPinNoVersionAndKeepPrivateTextInOnePlace: no user doc pins a
// concrete release, which would go stale with the next one; and the text
// that becomes wrong when the repository is made public lives only in the
// quoted note of docs/getting-started.md (and the release checklist that
// says to delete it), so the flip is one deletion.
func TestDocsPinNoVersionAndKeepPrivateTextInOnePlace(t *testing.T) {
	for _, doc := range userDocs(t) {
		name := filepath.Base(doc)
		for i, line := range strings.Split(readDoc(t, doc), "\n") {
			if versionPin.MatchString(line) {
				t.Errorf("%s:%d pins a version (use vX.Y.Z or @latest): %s", name, i+1, line)
			}
			inNote := name == "getting-started.md" && strings.HasPrefix(line, ">")
			if privateText.MatchString(line) && !inNote && name != "release.md" {
				t.Errorf("%s:%d mentions the private repository outside the note in getting-started.md: %s", name, i+1, line)
			}
		}
	}
}

// goNames indexes the module's Go source by package name: top-level
// declarations as "Name", methods and struct fields as "Type.Name".
func goNames(t *testing.T) map[string]map[string]bool {
	t.Helper()
	names := map[string]map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == "e2e" || strings.HasPrefix(d.Name(), ".")) && path != "../.." {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		pkg := names[f.Name.Name]
		if pkg == nil {
			pkg = map[string]bool{}
			names[f.Name.Name] = pkg
		}
		addDecls(pkg, f)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func addDecls(pkg map[string]bool, f *ast.File) {
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv != nil && len(d.Recv.List) == 1 {
				recv := d.Recv.List[0].Type
				if star, ok := recv.(*ast.StarExpr); ok {
					recv = star.X
				}
				pkg[typeName(recv)+"."+d.Name.Name] = true
			} else {
				pkg[d.Name.Name] = true
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					pkg[s.Name.Name] = true
					if st, ok := s.Type.(*ast.StructType); ok {
						for _, field := range st.Fields.List {
							for _, n := range field.Names {
								pkg[s.Name.Name+"."+n.Name] = true
							}
						}
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						pkg[n.Name] = true
					}
				}
			}
		}
	}
}

// goName matches pkg.Name, pkg.Type.Name and Type.Name inside a code span.
var goName = regexp.MustCompile(`\b([A-Za-z]\w*)\.([A-Za-z_]\w*)(?:\.([A-Za-z_]\w*))?`)

// TestArchitectureNamesExist: every internal/... path and every Go name of
// the form pkg.Name, pkg.Type.Name or Type.Name in the architecture guide's
// code spans exists in the code.
func TestArchitectureNamesExist(t *testing.T) {
	names := goNames(t)
	text := prose(readDoc(t, "../../docs/architecture.md"))
	checked := 0
	for _, span := range codeSpan.FindAllString(text, -1) {
		span = strings.Trim(span, "`")
		if strings.HasPrefix(span, "internal/") {
			if !strings.ContainsAny(span, "*<{") {
				checked++
				if _, err := os.Stat("../../" + span); err != nil {
					t.Errorf("docs/architecture.md: %s does not exist", span)
				}
			}
			continue
		}
		if strings.Contains(span, "/") || strings.HasPrefix(span, "git ") {
			continue // a path, or a git command (git config core.hooksPath)
		}
		for _, m := range goName.FindAllStringSubmatch(span, -1) {
			if ok, known := nameExists(names, m[1], m[2], m[3]); known {
				checked++
				if !ok {
					t.Errorf("docs/architecture.md: %s does not exist in the code", m[0])
				}
			}
		}
	}
	if checked < 30 {
		t.Errorf("checked only %d names; the span or name pattern may have stopped matching", checked)
	}
}

// fileExt are suffixes that make a.b a file name (README.md), not Go.
var fileExt = map[string]bool{"md": true, "go": true, "json": true, "yml": true, "sh": true, "js": true, "html": true, "css": true}

// nameExists reports whether a.b(.c) names something in the code. known is
// false for what is not about this module's Go code: file names and other
// modules' packages (os.Exit).
func nameExists(names map[string]map[string]bool, a, b, c string) (ok, known bool) {
	if fileExt[b] {
		return false, false
	}
	if pkg, isPkg := names[a]; isPkg {
		if c != "" {
			return pkg[b+"."+c], true
		}
		return pkg[b], true
	}
	if !unicode.IsUpper(rune(a[0])) {
		return false, false
	}
	for _, pkg := range names {
		ok = ok || (pkg[a] && pkg[a+"."+b])
	}
	return ok, true
}
