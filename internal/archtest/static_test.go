package archtest

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The page has no build step: a module whose import names a missing file or
// export fails in the browser only, and the CSP (default-src 'self') blocks
// markup and styles the page writes inline. These checks read the files the
// server embeds, so both kinds of mistake fail here first.

const staticRoot = "../web/static"

var (
	// importRe matches import [default,] {names} | * as ns from "spec" and
	// import "spec". Group 1: the default name, 2: the braces' content,
	// 3: the specifier.
	importRe     = regexp.MustCompile(`(?s)\bimport\s+(?:([A-Za-z_$][\w$]*)\s*,?\s*)?(?:\{([^}]*)\}|\*\s*as\s+[A-Za-z_$][\w$]*)?\s*(?:from\s*)?["']([^"']+)["']`)
	exportDeclRe = regexp.MustCompile(`\bexport\s+(?:async\s+)?(?:function\s*\*?|const|let|var|class)\s+([A-Za-z_$][\w$]*)`)
	exportListRe = regexp.MustCompile(`(?s)\bexport\s*\{([^}]*)\}`)
	exportDefRe  = regexp.MustCompile(`\bexport\s+default\b`)
)

// splitNames reads "a, b as c" into the names before "as" (local is
// false) or after it (local is true, the name the module exports).
func splitNames(list string, exported bool) []string {
	var out []string
	for _, part := range strings.Split(list, ",") {
		f := strings.Fields(part)
		switch {
		case len(f) == 0:
		case len(f) == 3 && f[1] == "as" && exported:
			out = append(out, f[2])
		default:
			out = append(out, f[0])
		}
	}
	return out
}

func exportsOf(src string) map[string]bool {
	out := map[string]bool{}
	for _, m := range exportDeclRe.FindAllStringSubmatch(src, -1) {
		out[m[1]] = true
	}
	for _, m := range exportListRe.FindAllStringSubmatch(src, -1) {
		for _, n := range splitNames(m[1], true) {
			out[n] = true
		}
	}
	if exportDefRe.MatchString(src) {
		out["default"] = true
	}
	return out
}

// importProblems checks every import in files (slash paths relative to the
// static root → content): the specifier is relative, names a file in files,
// and that file exports each imported name.
func importProblems(files map[string]string) []string {
	var out []string
	for name, src := range files {
		if !strings.HasSuffix(name, ".js") {
			continue
		}
		for _, m := range importRe.FindAllStringSubmatch(src, -1) {
			spec := m[3]
			if !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") {
				out = append(out, name+": import "+spec+": not a relative path")
				continue
			}
			target := path.Join(path.Dir(name), spec)
			tsrc, ok := files[target]
			if !ok {
				out = append(out, name+": import "+spec+": no file "+target)
				continue
			}
			want := splitNames(m[2], false)
			if m[1] != "" {
				want = append(want, "default")
			}
			have := exportsOf(tsrc)
			for _, n := range want {
				if !have[n] {
					out = append(out, name+": imports "+n+" from "+spec+", which does not export it")
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

type forbidden struct {
	what string
	re   *regexp.Regexp
	html bool // only in .html files
}

var (
	scriptRe   = regexp.MustCompile(`(?is)<script\b[^>]*>(.*?)</script>`)
	forbiddens = []forbidden{
		{"innerHTML", regexp.MustCompile(`\binnerHTML\b`), false},
		{"outerHTML", regexp.MustCompile(`\bouterHTML\b`), false},
		{"insertAdjacentHTML", regexp.MustCompile(`\binsertAdjacentHTML\b`), false},
		{"document.write", regexp.MustCompile(`\bdocument\s*\.\s*write(?:ln)?\b`), false},
		{"style attribute", regexp.MustCompile(`(?i)\sstyle=`), false},
		{"setAttribute(\"style\")", regexp.MustCompile(`setAttribute\s*\(\s*["'\x60]style["'\x60]`), false},
		{"<style> element", regexp.MustCompile(`(?i)<style\b`), false},
		{"inline event handler attribute", regexp.MustCompile(`(?i)\son[a-z]+\s*=`), true},
	}
)

func lineOf(src string, off int) int { return strings.Count(src[:off], "\n") + 1 }

// cspProblems lists what the CSP would block or what writes markup from a
// string, one entry per hit with its line.
func cspProblems(name, src string) []string {
	isHTML := strings.HasSuffix(name, ".html")
	var out []string
	for _, f := range forbiddens {
		if f.html && !isHTML {
			continue
		}
		for _, loc := range f.re.FindAllStringIndex(src, -1) {
			out = append(out, name+":"+strconv.Itoa(lineOf(src, loc[0]))+": "+f.what)
		}
	}
	if isHTML {
		for _, m := range scriptRe.FindAllStringSubmatchIndex(src, -1) {
			if strings.TrimSpace(src[m[2]:m[3]]) != "" {
				out = append(out, name+":"+strconv.Itoa(lineOf(src, m[0]))+": inline <script> content")
			}
		}
	}
	return out
}

func TestImportProblems(t *testing.T) {
	files := map[string]string{
		"js/a.js": `import { one, two as t } from "./b.js";
import * as all from "./b.js";
import def, { three } from "./c.js";
import {
  four,
} from "./c.js";
import "./side.js";`,
		"js/b.js":    "export function one() {}\nexport async function two() {}\n",
		"js/c.js":    "const x = 1;\nexport { x as three, four };\nexport default x;\n",
		"js/side.js": "",
		"test/t.js":  `import { one } from "../js/b.js";`,
	}
	if got := importProblems(files); len(got) != 0 {
		t.Fatalf("clean tree flagged: %q", got)
	}
	files["js/d.js"] = `import { gone } from "./b.js";
import { x } from "./missing.js";
import { y } from "lib";
import { four as f, x as z } from "./c.js";`
	want := []string{
		"js/d.js: import ./missing.js: no file js/missing.js",
		"js/d.js: import lib: not a relative path",
		"js/d.js: imports gone from ./b.js, which does not export it",
		"js/d.js: imports x from ./c.js, which does not export it",
	}
	if got := importProblems(files); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestCSPProblems(t *testing.T) {
	clean := `<script type="module" src="/js/main.js"></script>
<link rel="stylesheet" href="/css/app.css"><a href="#x">x</a>`
	if got := cspProblems("ok.html", clean); len(got) != 0 {
		t.Fatalf("clean html flagged: %q", got)
	}
	okJS := "el.style.width = `${w}px`;\nel.addEventListener(\"click\", f);\nconst onClick = 1;\n"
	if got := cspProblems("ok.js", okJS); len(got) != 0 {
		t.Fatalf("clean js flagged: %q", got)
	}
	for _, c := range []struct{ name, src, want string }{
		{"a.js", "el.innerHTML = s;", "a.js:1: innerHTML"},
		{"a.js", "x;\nel.outerHTML = s;", "a.js:2: outerHTML"},
		{"a.js", "el.insertAdjacentHTML('beforeend', s);", "a.js:1: insertAdjacentHTML"},
		{"a.js", "document.write(s);", "a.js:1: document.write"},
		{"a.js", "el.setAttribute(\"style\", s);", "a.js:1: setAttribute(\"style\")"},
		{"a.html", `<div style="color:red"></div>`, "a.html:1: style attribute"},
		{"a.html", "<style>p{}</style>", "a.html:1: <style> element"},
		{"a.html", `<button onclick="f()">x</button>`, "a.html:1: inline event handler attribute"},
		{"a.html", "<script>\nrun();\n</script>", "a.html:1: inline <script> content"},
	} {
		if got := cspProblems(c.name, c.src); len(got) != 1 || got[0] != c.want {
			t.Errorf("%q: got %q want [%q]", c.src, got, c.want)
		}
	}
}

func readStatic(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(staticRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if ext := filepath.Ext(p); ext != ".js" && ext != ".html" {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(staticRoot, p)
		files[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestStaticImportsResolve and TestStaticCSPPatterns read the real tree. A
// tree without main.js means the walk read the wrong directory.
func TestStaticImportsResolve(t *testing.T) {
	files := readStatic(t)
	if _, ok := files["js/main.js"]; !ok {
		t.Fatalf("no js/main.js under %s: read %d files", staticRoot, len(files))
	}
	// main.js imports every tab: fewer matches means the pattern went blind.
	if n := len(importRe.FindAllString(files["js/main.js"], -1)); n < 8 {
		t.Fatalf("matched %d imports in js/main.js, want at least 8", n)
	}
	for _, p := range importProblems(files) {
		t.Error(p)
	}
}

func TestStaticCSPPatterns(t *testing.T) {
	files := readStatic(t)
	if _, ok := files["index.html"]; !ok {
		t.Fatalf("no index.html under %s", staticRoot)
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		for _, p := range cspProblems(n, files[n]) {
			t.Error(p)
		}
	}
}
