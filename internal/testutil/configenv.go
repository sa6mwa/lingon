package testutil

import (
	"path/filepath"
	"testing"
)

// SetXDGConfigEnv points Lingon config/state/cache env vars at an isolated temp root.
func SetXDGConfigEnv(t *testing.T) string {
	t.Helper()
	root := TempDir(t)
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, ".cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, ".state"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".local", "share"))
	return root
}
