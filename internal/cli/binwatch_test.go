package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeExe(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func watchOf(t *testing.T, path string, stat func(string) (fs.FileInfo, error)) *binaryWatch {
	t.Helper()
	w, err := newBinaryWatch(path, stat)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestBinaryWatchUnchangedIsFalse(t *testing.T) {
	p := filepath.Join(t.TempDir(), "modelpick")
	writeExe(t, p, "v1")
	if watchOf(t, p, os.Stat).Replaced() {
		t.Fatal("Replaced() = true for the file as it was at start")
	}
}

func TestBinaryWatchRenamedOverIsTrue(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "modelpick")
	writeExe(t, p, "v1")
	w := watchOf(t, p, os.Stat)
	// Same size and mtime as the original: only the inode differs, as in
	// scripts/deploy-local.sh.
	old, _ := os.Stat(p)
	tmp := filepath.Join(dir, "modelpick.new")
	writeExe(t, tmp, "v2")
	if err := os.Chtimes(tmp, old.ModTime(), old.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, p); err != nil {
		t.Fatal(err)
	}
	if !w.Replaced() {
		t.Fatal("Replaced() = false after a rename over the file")
	}
}

func TestBinaryWatchRemovedIsTrue(t *testing.T) {
	p := filepath.Join(t.TempDir(), "modelpick")
	writeExe(t, p, "v1")
	w := watchOf(t, p, os.Stat)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if !w.Replaced() {
		t.Fatal("Replaced() = false after the file was removed")
	}
}

func TestBinaryWatchSizeOrMtimeChangeIsTrue(t *testing.T) {
	p := filepath.Join(t.TempDir(), "modelpick")
	writeExe(t, p, "v1")
	w := watchOf(t, p, os.Stat)
	writeExe(t, p, "version two") // same inode, larger
	if !w.Replaced() {
		t.Fatal("Replaced() = false after the size changed")
	}
	p2 := filepath.Join(t.TempDir(), "modelpick")
	writeExe(t, p2, "v1")
	w2 := watchOf(t, p2, os.Stat)
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(p2, later, later); err != nil {
		t.Fatal(err)
	}
	if !w2.Replaced() {
		t.Fatal("Replaced() = false after the mtime changed")
	}
}

func TestBinaryWatchStatErrorIsUnknownNotTrue(t *testing.T) {
	p := filepath.Join(t.TempDir(), "modelpick")
	writeExe(t, p, "v1")
	fail := false
	stat := func(name string) (fs.FileInfo, error) {
		if fail {
			return nil, &fs.PathError{Op: "stat", Path: name, Err: errors.New("permission denied")}
		}
		return os.Stat(name)
	}
	w := watchOf(t, p, stat)
	fail = true
	if w.Replaced() {
		t.Fatal("Replaced() = true on a stat error that is not not-exist")
	}
}

func TestBinaryWatchTrueStaysTrue(t *testing.T) {
	p := filepath.Join(t.TempDir(), "modelpick")
	writeExe(t, p, "v1")
	orig, _ := os.ReadFile(p)
	st, _ := os.Stat(p)
	w := watchOf(t, p, os.Stat)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if !w.Replaced() {
		t.Fatal("not replaced after removal")
	}
	// Put back a file with the original bytes and mtime, as a rollback would.
	if err := os.WriteFile(p, orig, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	if !w.Replaced() {
		t.Fatal("Replaced() went back to false")
	}
}

func TestBinaryWatchNoStartIdentityIsAlwaysFalse(t *testing.T) {
	// The watch cannot say what it started as: never show the notice.
	f := binaryReplacedFunc(func() (string, error) { return "", errors.New("no executable") }, os.Stat)
	if f() {
		t.Fatal("true without a starting identity")
	}
}
