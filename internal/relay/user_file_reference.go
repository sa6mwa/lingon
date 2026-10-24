package relay

import (
	"fmt"
	"path/filepath"
)

// A directory identity plus relative filename identifies a write target even
// through directory aliases/bind mounts. Unlike a file inode, it remains valid
// when an atomic users-file save replaces the file.
type userFileReference struct {
	Device   uint64 `json:"device"`
	Inode    uint64 `json:"inode"`
	Relative string `json:"relative"`
}

func newUserFileReference(path string) (userFileReference, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return userFileReference{}, err
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		device, inode, err := directoryIdentity(parent)
		if err == nil {
			relative, err := filepath.Rel(parent, path)
			return userFileReference{Device: device, Inode: inode, Relative: relative}, err
		}
		if parent == filepath.Dir(parent) {
			return userFileReference{}, fmt.Errorf("identify users-file directory: %w", err)
		}
	}
}

func (ref userFileReference) matches(path string) bool {
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		device, inode, err := directoryIdentity(parent)
		if err == nil && device == ref.Device && inode == ref.Inode {
			relative, err := filepath.Rel(parent, path)
			return err == nil && relative == ref.Relative
		}
		if parent == filepath.Dir(parent) {
			return false
		}
	}
}
