package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// binaryWatch tells whether the executable file `modelpick ui` started from
// is still the one on disk. scripts/deploy-local.sh installs a new binary by
// renaming a new file over the old path, so the inode changes; a removed
// file, or a changed size or mtime, counts too.
type binaryWatch struct {
	path string
	stat func(string) (fs.FileInfo, error)
	orig fs.FileInfo

	mu       sync.Mutex
	replaced bool // once true, true for the run
}

// newBinaryWatch records path's identity now.
func newBinaryWatch(path string, stat func(string) (fs.FileInfo, error)) (*binaryWatch, error) {
	fi, err := stat(path)
	if err != nil {
		return nil, err
	}
	return &binaryWatch{path: path, stat: stat, orig: fi}, nil
}

// Replaced re-stats the file. A stat error other than not-exist is unknown
// and leaves the answer as it was, never a guess.
func (w *binaryWatch) Replaced() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.replaced {
		return true
	}
	fi, err := w.stat(w.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		w.replaced = true
	case err != nil:
		// unknown
	case !os.SameFile(w.orig, fi) || fi.Size() != w.orig.Size() || !fi.ModTime().Equal(w.orig.ModTime()):
		w.replaced = true
	}
	return w.replaced
}

// binaryReplacedFunc watches the executable exe names, symlinks resolved. If
// the starting identity cannot be taken, the func is always false: with no
// baseline the answer is unknown, and unknown shows nothing.
func binaryReplacedFunc(exe func() (string, error), stat func(string) (fs.FileInfo, error)) func() bool {
	never := func() bool { return false }
	p, err := exe()
	if err != nil {
		return never
	}
	w, err := newBinaryWatch(p, stat)
	if err != nil {
		return never
	}
	return w.Replaced
}

// executablePath is this process's executable with symlinks resolved.
func executablePath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}
