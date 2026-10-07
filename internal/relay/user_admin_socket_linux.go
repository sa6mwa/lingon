package relay

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type fdUserAdminSocketAddress struct {
	path string
	file *os.File
}

func (a *fdUserAdminSocketAddress) Path() string { return a.path }
func (a *fdUserAdminSocketAddress) Close() error { return a.file.Close() }

func newLongUserAdminSocketAddress(path string) (userAdminSocketAddress, error) {
	parent := filepath.Dir(path)
	fd, err := unix.Open(parent, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), parent)
	return &fdUserAdminSocketAddress{
		path: fmt.Sprintf("/proc/self/fd/%d/%s", fd, filepath.Base(path)), file: file,
	}, nil
}
