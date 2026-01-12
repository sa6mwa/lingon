package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	expectedDir := filepath.Join(home, DefaultConfigDirName)
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

	expectedLog := filepath.Join(expectedDir, DefaultLogFileName)
	if got := DefaultLogPath(); got != expectedLog {
		t.Fatalf("DefaultLogPath() = %q, want %q", got, expectedLog)
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
