package config

import (
	"path/filepath"
	"testing"

	"pkt.systems/lingon/internal/testutil"
)

func TestDefaultPaths(t *testing.T) {
	root := testutil.SetXDGConfigEnv(t)

	expectedDir := filepath.Join(root, "lingon")
	if got := DefaultConfigDir(); got != expectedDir {
		t.Fatalf("DefaultConfigDir() = %q, want %q", got, expectedDir)
	}

	expectedConfig := filepath.Join(expectedDir, DefaultConfigFileName)
	if got := DefaultConfigPath(); got != expectedConfig {
		t.Fatalf("DefaultConfigPath() = %q, want %q", got, expectedConfig)
	}

	expectedAuth := filepath.Join(expectedDir, DefaultAuthFileName)
	if got := DefaultAuthPath(); got != expectedAuth {
		t.Fatalf("DefaultAuthPath() = %q, want %q", got, expectedAuth)
	}

	if got := DefaultLogPath(); got != "" {
		t.Fatalf("DefaultLogPath() = %q, want empty string", got)
	}

	expectedTLSDir := filepath.Join(expectedDir, DefaultTLSDirName)
	if got := DefaultTLSDir(); got != expectedTLSDir {
		t.Fatalf("DefaultTLSDir() = %q, want %q", got, expectedTLSDir)
	}

	expectedCache := filepath.Join(expectedTLSDir, DefaultTLSCacheDirName)
	if got := DefaultTLSCacheDir(); got != expectedCache {
		t.Fatalf("DefaultTLSCacheDir() = %q, want %q", got, expectedCache)
	}
}
