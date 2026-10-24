package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// WriteFileAtomic replaces path with data so a reader sees the old or the new
// content, never a mix. A symlink at path is followed: the link stays, its
// target gets the data. The temp file is written in the target's directory,
// synced, then renamed. The mode is the existing target's, else 0600. On any
// error the temp file is removed and the target is untouched.
func WriteFileAtomic(path string, data []byte) error {
	target, err := resolveTarget(path)
	if err != nil {
		return err
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

const maxSymlinkHops = 40

// resolveTarget is the file WriteFileAtomic must replace: path with symlinks
// followed. A link whose target does not exist yet (EvalSymlinks fails) is
// followed by hand so the link stays a link and the target is created.
func resolveTarget(path string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, nil
	}
	cur := path
	for hops := 0; hops <= maxSymlinkHops; hops++ {
		fi, err := os.Lstat(cur)
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			return cur, nil // missing or a regular file: write it
		}
		dest, err := os.Readlink(cur)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(dest) {
			dest = filepath.Join(filepath.Dir(cur), dest)
		}
		cur = dest
	}
	return "", fmt.Errorf("%s: too many symlinks", path)
}
