package platform

import (
	"errors"
	"os"
	"path/filepath"
)

// WriteFileAtomic replaces path with data so a reader sees the old or the new
// content, never a mix. A symlink at path is followed: the link stays, its
// target gets the data. The temp file is written in the target's directory,
// synced, then renamed. The mode is the existing target's, else 0600. On any
// error the temp file is removed and the target is untouched.
func WriteFileAtomic(path string, data []byte) error {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(target); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if err := writeSyncClose(tmp, data, mode); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func writeSyncClose(f *os.File, data []byte, mode os.FileMode) error {
	_, werr := f.Write(data)
	var merr, serr error
	if werr == nil {
		merr = f.Chmod(mode)
		serr = f.Sync()
	}
	return errors.Join(werr, merr, serr, f.Close())
}
