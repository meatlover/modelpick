package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileAtomicFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real", "target.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := WriteFileAtomic(link, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link must stay a symlink: %v, %v", fi, err)
	}
	if b, _ := os.ReadFile(target); string(b) != "new" {
		t.Errorf("target = %q, want new", b)
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640", fi.Mode().Perm())
	}
	if ents, _ := os.ReadDir(filepath.Dir(target)); len(ents) != 1 {
		t.Errorf("temp file left behind: %v", ents)
	}
}

func TestWriteFileAtomicNew(t *testing.T) {
	p := filepath.Join(t.TempDir(), "n.json")
	if err := WriteFileAtomic(p, []byte("x")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("stat = %v, %v; want mode 0600", fi, err)
	}
}

func TestWriteFileAtomicErrorLeavesTargetAndNoTemp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "missing-dir", "f.json")
	if err := WriteFileAtomic(p, []byte("x")); err == nil {
		t.Fatal("want an error for a missing directory")
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("unexpected entries: %v", ents)
	}
}

func mustSymlink(t *testing.T, old, link string) {
	t.Helper()
	if err := os.Symlink(old, link); err != nil {
		t.Fatal(err)
	}
}

func assertLinkAndBytes(t *testing.T, links []string, final, want string) {
	t.Helper()
	for _, l := range links {
		if fi, err := os.Lstat(l); err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s must stay a symlink: %v, %v", l, fi, err)
		}
	}
	b, err := os.ReadFile(final)
	if err != nil || string(b) != want {
		t.Errorf("%s = %q, %v; want %q", final, b, err, want)
	}
	if fi, err := os.Stat(final); err == nil && fi.Mode().Perm() != 0o600 {
		t.Errorf("new target mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestWriteFileAtomicDanglingAbsoluteLink(t *testing.T) {
	dir := t.TempDir()
	final, link := filepath.Join(dir, "final.json"), filepath.Join(dir, "link.json")
	mustSymlink(t, final, link)
	if err := WriteFileAtomic(link, []byte("new")); err != nil {
		t.Fatal(err)
	}
	assertLinkAndBytes(t, []string{link}, final, "new")
}

func TestWriteFileAtomicDanglingRelativeLink(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	mustSymlink(t, filepath.Join("sub", "final.json"), link)
	if err := WriteFileAtomic(link, []byte("new")); err != nil {
		t.Fatal(err)
	}
	assertLinkAndBytes(t, []string{link}, filepath.Join(dir, "sub", "final.json"), "new")
}

func TestWriteFileAtomicTwoHopChain(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "final.json")
	l1, l2 := filepath.Join(dir, "l1.json"), filepath.Join(dir, "l2.json")
	mustSymlink(t, "l2.json", l1)
	mustSymlink(t, final, l2)
	if err := WriteFileAtomic(l1, []byte("new")); err != nil {
		t.Fatal(err)
	}
	assertLinkAndBytes(t, []string{l1, l2}, final, "new")
}

func TestWriteFileAtomicSymlinkLoop(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	mustSymlink(t, "b", a)
	mustSymlink(t, "a", b)
	if err := WriteFileAtomic(a, []byte("x")); err == nil || !strings.Contains(err.Error(), "too many symlinks") {
		t.Fatalf("err = %v, want too many symlinks", err)
	}
}

func TestWriteFileAtomicRenameFailureCleansUp(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "t.json")
	if err := os.MkdirAll(filepath.Join(target, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(target, []byte("x")); err == nil {
		t.Fatal("want an error when the target is a non-empty directory")
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(m) != 0 {
		t.Errorf("stray temp files: %v", m)
	}
	if fi, err := os.Stat(filepath.Join(target, "inner")); err != nil || !fi.IsDir() {
		t.Errorf("target untouched: %v, %v", fi, err)
	}
}
