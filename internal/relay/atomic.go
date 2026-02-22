package relay

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

var atomicRename = os.Rename

func writeFileAtomic(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := writeFileAtomicOnce(path, data, perm); err != nil {
			lastErr = err
			if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			continue
		}
		return nil
	}
	return lastErr
}

func writeFileAtomicOnce(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if err := tmp.Chmod(perm); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := atomicRename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
