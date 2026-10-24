package relay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
)

type userAdminSocketAddress interface {
	Path() string
	Close() error
}

type directUserAdminSocketAddress string

func (a directUserAdminSocketAddress) Path() string { return string(a) }
func (a directUserAdminSocketAddress) Close() error { return nil }

func newUserAdminSocketAddress(path string) (userAdminSocketAddress, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	// Use the strictest shipped Unix address limit without changing short-path
	// operation in environments without /proc or a writable temporary root.
	if len(path) < 100 {
		return directUserAdminSocketAddress(path), nil
	}
	return newLongUserAdminSocketAddress(path)
}

func dialUserAdminSocket(ctx context.Context, path string) (net.Conn, error) {
	address, err := newUserAdminSocketAddress(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = address.Close() }()
	return (&net.Dialer{}).DialContext(ctx, "unix", address.Path())
}

type userAdminUnixListener struct {
	*net.UnixListener
	path      string
	info      os.FileInfo
	closeOnce sync.Once
	closeErr  error
}

func newUserAdminUnixListener(path string) (net.Listener, error) {
	address, err := newUserAdminSocketAddress(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = address.Close() }()
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: address.Path(), Net: "unix"})
	if err != nil {
		return nil, err
	}
	listener.SetUnlinkOnClose(false)
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		_ = listener.Close()
		if err == nil {
			err = fmt.Errorf("local administration path is not a socket")
		}
		return nil, err
	}
	return &userAdminUnixListener{UnixListener: listener, path: path, info: info}, nil
}

func (l *userAdminUnixListener) Addr() net.Addr {
	return &net.UnixAddr{Name: l.path, Net: "unix"}
}

func (l *userAdminUnixListener) Close() error {
	l.closeOnce.Do(func() {
		l.closeErr = l.UnixListener.Close()
		info, err := os.Lstat(l.path)
		if os.IsNotExist(err) {
			return
		}
		if err != nil {
			l.closeErr = errors.Join(l.closeErr, err)
			return
		}
		if !os.SameFile(l.info, info) {
			l.closeErr = errors.Join(l.closeErr, fmt.Errorf("local administration socket was replaced"))
			return
		}
		l.closeErr = errors.Join(l.closeErr, os.Remove(l.path))
	})
	return l.closeErr
}
