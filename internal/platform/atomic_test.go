package platform

import (
	"os"
	"path/filepath"
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
