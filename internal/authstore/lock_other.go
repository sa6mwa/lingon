//go:build !unix && !windows

package authstore

import (
	"os"
	"path/filepath"
)

type fileLock struct {
	file *os.File
}

func lockFile(path string) (*fileLock, error) {
	lockPath := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	return &fileLock{file: f}, nil
}

func (l *fileLock) unlock() error {
	if l == nil || l.file == nil {
		return nil
	}
	name := l.file.Name()
	err := l.file.Close()
	if removeErr := os.Remove(name); err == nil {
		err = removeErr
	}
	return err
}
