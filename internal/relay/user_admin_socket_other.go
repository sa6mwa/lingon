//go:build !linux

package relay

import (
	"errors"
	"os"
	"path/filepath"
)

type aliasedUserAdminSocketAddress struct {
	path string
	dir  string
}

func (a *aliasedUserAdminSocketAddress) Path() string { return a.path }

// Other shipped Unix targets provide /tmp; TMPDIR may itself be a long path.
func newLongUserAdminSocketAddress(path string) (userAdminSocketAddress, error) {
	dir, err := os.MkdirTemp("/tmp", "lingon-admin-")
	if err != nil {
		return nil, err
	}
	address := &aliasedUserAdminSocketAddress{path: filepath.Join(dir, "dir", filepath.Base(path)), dir: dir}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, errors.Join(err, address.Close())
	}
	if err := os.Symlink(filepath.Dir(path), filepath.Join(dir, "dir")); err != nil {
		return nil, errors.Join(err, address.Close())
	}
	return address, nil
}

func (a *aliasedUserAdminSocketAddress) Close() error {
	linkErr := os.Remove(filepath.Join(a.dir, "dir"))
	if os.IsNotExist(linkErr) {
		linkErr = nil
	}
	dirErr := os.Remove(a.dir)
	if os.IsNotExist(dirErr) {
		dirErr = nil
	}
	return errors.Join(linkErr, dirErr)
}
