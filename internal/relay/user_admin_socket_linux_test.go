package relay

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLongUserAdminSocketAddressClosesItsDirectoryDescriptor(t *testing.T) {
	dir := filepath.Join(t.TempDir(), strings.Repeat("private-", 12))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	address, err := newUserAdminSocketAddress(filepath.Join(dir, "socket"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = address.Close() })
	fdAddress, ok := address.(*fdUserAdminSocketAddress)
	if !ok {
		t.Fatal("long Linux path did not use a directory descriptor")
	}
	if err := address.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := fdAddress.file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("directory descriptor was not closed: %v", err)
	}
}
