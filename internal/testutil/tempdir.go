package testutil

import (
	"os"
	"testing"
)

// TempDir creates an isolated per-test temp directory and cleans it up.
func TempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "lingon-test-")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir
}
