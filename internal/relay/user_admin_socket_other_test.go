//go:build !linux && !windows

package relay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLongUnixUserAdminSocketAddressRemovesItsPrivateAlias(t *testing.T) {
	dir := filepath.Join(t.TempDir(), strings.Repeat("private-", 12))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	address, err := newUserAdminSocketAddress(filepath.Join(dir, "socket"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = address.Close() })
	alias, ok := address.(*aliasedUserAdminSocketAddress)
	if !ok {
		t.Fatal("long Unix path did not use a private directory alias")
	}
	info, err := os.Stat(alias.dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("address alias is not private: %v", err)
	}
	if err := address.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(alias.dir); !os.IsNotExist(err) {
		t.Fatalf("address alias was not removed: %v", err)
	}
}
