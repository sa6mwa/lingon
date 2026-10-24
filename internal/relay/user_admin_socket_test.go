//go:build !windows

package relay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUserAdminSocketAddressIsPrivateBoundedAndTemporary(t *testing.T) {
	dir := filepath.Join(t.TempDir(), strings.Repeat("long-", 12), strings.Repeat("temp-", 12))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", dir)
	path := filepath.Join(dir, "socket")
	address, err := newUserAdminSocketAddress(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = address.Close() })
	if len(address.Path()) >= 100 {
		t.Fatal("socket address depends on a long data directory or TMPDIR")
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("socket alias is not private: %v", err)
	}
	if target, err := os.Stat(filepath.Dir(address.Path())); err != nil || !os.SameFile(info, target) {
		t.Fatalf("socket address points to the wrong private data directory: %v", err)
	}
	if err := address.Close(); err != nil {
		t.Fatal(err)
	}
	listener, err := newUserAdminUnixListener(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if listener.Addr().String() != path {
		t.Fatal("listener did not report its actual data-directory socket")
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("listener left its actual socket behind: %v", err)
	}
}

func TestUserAdminSocketCleanupPreservesReplacementFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "socket")
	listener, err := newUserAdminUnixListener(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err == nil {
		t.Fatal("socket replacement was not reported")
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "replacement" {
		t.Fatalf("socket cleanup removed a foreign replacement: %v", err)
	}
}
