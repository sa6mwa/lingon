package testutil

import (
	"path/filepath"
	"testing"
)

// SetLingonConfigEnv points Lingon config paths at an isolated config directory.
func SetLingonConfigEnv(t *testing.T) string {
	t.Helper()
	root := TempDir(t)
	cfgDir := filepath.Join(root, ".lingon")
	t.Setenv("LINGON_CONFIG_DIR", cfgDir)
	return cfgDir
}
