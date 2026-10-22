//go:build unix

package platform

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// LockFile takes an exclusive flock on path, creating the file (0600) and its
// parent directory (0700) when missing. It blocks until the lock is free.
// unlock releases it; the kernel also releases it if the process dies, so a
// lock can never go stale.
func LockFile(path string) (unlock func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		return nil, &os.PathError{Op: "flock", Path: path, Err: err}
	}
	return func() { _ = f.Close() }, nil
}
