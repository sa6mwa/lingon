package config

import (
	"os"
	"path/filepath"
	"testing"

	"pkt.systems/lingon/internal/testutil"
)

func TestLoaderResolvesRelativePathsFromConfig(t *testing.T) {
	dir := testutil.TempDir(t)
	configPath := filepath.Join(dir, "config.yaml")

	configData := []byte(`server:
  data_dir: data
  users_file: users.json
  tls:
    dir: tls
    cache_dir: tls/cache
client:
  auth_file: auth.json
  log_file: ""
`)
	if err := os.WriteFile(configPath, configData, 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	loader := NewLoader()
	loader.SetConfigFile(configPath)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.DataDir != filepath.Join(dir, "data") {
		t.Fatalf("DataDir = %q, want %q", cfg.Server.DataDir, filepath.Join(dir, "data"))
	}
	if cfg.Server.UsersFile != filepath.Join(dir, "users.json") {
		t.Fatalf("UsersFile = %q, want %q", cfg.Server.UsersFile, filepath.Join(dir, "users.json"))
	}
	if cfg.Server.TLS.Dir != filepath.Join(dir, "tls") {
		t.Fatalf("TLS.Dir = %q, want %q", cfg.Server.TLS.Dir, filepath.Join(dir, "tls"))
	}
	if cfg.Server.TLS.CacheDir != filepath.Join(dir, "tls", "cache") {
		t.Fatalf("TLS.CacheDir = %q, want %q", cfg.Server.TLS.CacheDir, filepath.Join(dir, "tls", "cache"))
	}
	if cfg.Client.AuthFile != filepath.Join(dir, "auth.json") {
		t.Fatalf("Client.AuthFile = %q, want %q", cfg.Client.AuthFile, filepath.Join(dir, "auth.json"))
	}
	if cfg.Client.LogFile != "" {
		t.Fatalf("Client.LogFile = %q, want empty", cfg.Client.LogFile)
	}
}
