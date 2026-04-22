package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

const baseTempDirName = "lingontest"

// TempDir creates a temp directory under /tmp/lingontest and cleans it up.
func TempDir(t *testing.T) string {
	t.Helper()
	baseTempDir := filepath.Join(os.TempDir(), baseTempDirName)
	if err := os.MkdirAll(baseTempDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", baseTempDir, err)
	}
	dir, err := os.MkdirTemp(baseTempDir, "test-")
	if err != nil {
		t.Fatalf("mktemp %s: %v", baseTempDir, err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir
}
