package config

import (
	"path/filepath"
	"testing"

	"pkt.systems/lingon/internal/testutil"
)

func TestDefaultPaths(t *testing.T) {
	root := testutil.TempDir(t)
	t.Setenv("HOME", root)
	t.Setenv(ConfigDirEnv, "")

	expectedDir := filepath.Join(root, DefaultConfigDirName)
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

func TestDefaultPathsIgnoreXDGConfigHome(t *testing.T) {
	home := testutil.TempDir(t)
	xdg := testutil.TempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv(ConfigDirEnv, "")

	want := filepath.Join(home, ".lingon")
	if got := DefaultConfigDir(); got != want {
		t.Fatalf("DefaultConfigDir() = %q, want %q", got, want)
	}
	if got := DefaultAuthPath(); got != filepath.Join(want, DefaultAuthFileName) {
		t.Fatalf("DefaultAuthPath() = %q, want %q", got, filepath.Join(want, DefaultAuthFileName))
	}
	if got := DefaultConfigDir(); got == filepath.Join(xdg, ".lingon") || got == filepath.Join(xdg, "lingon") {
		t.Fatalf("DefaultConfigDir() used XDG_CONFIG_HOME: %q", got)
	}
}

func TestDefaultPathsUseLingonConfigDirEnv(t *testing.T) {
	home := testutil.TempDir(t)
	xdg := testutil.TempDir(t)
	cfgDir := filepath.Join(testutil.TempDir(t), "cfg")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv(ConfigDirEnv, cfgDir)

	if got := DefaultConfigDir(); got != cfgDir {
		t.Fatalf("DefaultConfigDir() = %q, want %q", got, cfgDir)
	}
	if got := DefaultConfigPath(); got != filepath.Join(cfgDir, DefaultConfigFileName) {
		t.Fatalf("DefaultConfigPath() = %q, want %q", got, filepath.Join(cfgDir, DefaultConfigFileName))
	}
	if got := DefaultAuthPath(); got != filepath.Join(cfgDir, DefaultAuthFileName) {
		t.Fatalf("DefaultAuthPath() = %q, want %q", got, filepath.Join(cfgDir, DefaultAuthFileName))
	}
	if got := DefaultTLSDir(); got != filepath.Join(cfgDir, DefaultTLSDirName) {
		t.Fatalf("DefaultTLSDir() = %q, want %q", got, filepath.Join(cfgDir, DefaultTLSDirName))
	}
}
