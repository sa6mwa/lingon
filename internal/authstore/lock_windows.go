//go:build windows

package authstore

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

type fileLock struct {
	file *os.File
}

func lockFile(path string) (*fileLock, error) {
	lockPath := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	var overlapped windows.Overlapped
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlapped)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &fileLock{file: f}, nil
}

func (l *fileLock) unlock() error {
	if l == nil || l.file == nil {
		return nil
	}
	var overlapped windows.Overlapped
	_ = windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, &overlapped)
	return l.file.Close()
}
