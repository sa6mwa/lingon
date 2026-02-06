package lingon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v3"

	"pkt.systems/lingon/internal/testutil"
	"pkt.systems/lingon/internal/tlsmgr"
)

func TestBootstrapWritesRelativeConfig(t *testing.T) {
	dir := testutil.TempDir(t)
	configPath := filepath.Join(dir, "config.yaml")

	cfg := BootstrapConfig()
	_, err := BootstrapWithOptions(context.Background(), BootstrapOptions{
		ConfigPath: configPath,
		Config:     cfg,
	})
	if err != nil {
		t.Fatalf("BootstrapWithOptions() error = %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", configPath, err)
	}

	var got Config
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("yaml.Unmarshal error = %v", err)
	}

	if got.Server.DataDir != "." {
		t.Fatalf("Server.DataDir = %q, want .", got.Server.DataDir)
	}
	if got.Server.UsersFile != DefaultUsersFileName {
		t.Fatalf("Server.UsersFile = %q, want %q", got.Server.UsersFile, DefaultUsersFileName)
	}
	if got.Server.TLS.Dir != DefaultTLSDirName {
		t.Fatalf("Server.TLS.Dir = %q, want %q", got.Server.TLS.Dir, DefaultTLSDirName)
	}
	if got.Server.TLS.CacheDir != filepath.Join(DefaultTLSDirName, DefaultTLSCacheDirName) {
		t.Fatalf("Server.TLS.CacheDir = %q, want %q", got.Server.TLS.CacheDir, filepath.Join(DefaultTLSDirName, DefaultTLSCacheDirName))
	}
	if got.Client.AuthFile != DefaultAuthFileName {
		t.Fatalf("Client.AuthFile = %q, want %q", got.Client.AuthFile, DefaultAuthFileName)
	}
}

func TestBootstrapSkipsWhenConfigExists(t *testing.T) {
	dir := testutil.TempDir(t)
	configPath := filepath.Join(dir, "config.yaml")

	original := []byte("server:\n  base: /original\n")
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	_, err := BootstrapWithOptions(context.Background(), BootstrapOptions{
		ConfigPath: configPath,
		Config:     BootstrapConfig(),
	})
	if err != nil {
		t.Fatalf("BootstrapWithOptions() error = %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}
	if string(data) != string(original) {
		t.Fatalf("config overwritten; got %q want %q", string(data), string(original))
	}
}

func TestBootstrapForceDoesNotOverwriteCertificates(t *testing.T) {
	dir := testutil.TempDir(t)
	configPath := filepath.Join(dir, "config.yaml")
	tlsDir := filepath.Join(dir, "tls")

	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll error = %v", err)
	}

	before, err := os.ReadFile(filepath.Join(tlsDir, "server.pem"))
	if err != nil {
		t.Fatalf("ReadFile(server.pem) error = %v", err)
	}

	cfg := BootstrapConfig()
	_, err = BootstrapWithOptions(context.Background(), BootstrapOptions{
		ConfigPath: configPath,
		Config:     cfg,
		Force:      true,
	})
	if err != nil {
		t.Fatalf("BootstrapWithOptions() error = %v", err)
	}

	after, err := os.ReadFile(filepath.Join(tlsDir, "server.pem"))
	if err != nil {
		t.Fatalf("ReadFile(server.pem) error = %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("server.pem changed during force bootstrap")
	}
}

func TestBootstrapRegeneratesCertificates(t *testing.T) {
	dir := testutil.TempDir(t)
	configPath := filepath.Join(dir, "config.yaml")
	tlsDir := filepath.Join(dir, "tls")

	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll error = %v", err)
	}

	before, err := os.ReadFile(filepath.Join(tlsDir, "ca.pem"))
	if err != nil {
		t.Fatalf("ReadFile(ca.pem) error = %v", err)
	}

	cfg := BootstrapConfig()
	_, err = BootstrapWithOptions(context.Background(), BootstrapOptions{
		ConfigPath:             configPath,
		Config:                 cfg,
		RegenerateCertificates: true,
	})
	if err != nil {
		t.Fatalf("BootstrapWithOptions() error = %v", err)
	}

	after, err := os.ReadFile(filepath.Join(tlsDir, "ca.pem"))
	if err != nil {
		t.Fatalf("ReadFile(ca.pem) error = %v", err)
	}
	if string(after) == string(before) {
		t.Fatalf("ca.pem did not change during regeneration")
	}
}

func TestBootstrapSkipsTLSWhenModeAcme(t *testing.T) {
	dir := testutil.TempDir(t)
	configPath := filepath.Join(dir, "config.yaml")

	cfg := BootstrapConfig()
	cfg.Server.TLS.Mode = "acme"

	_, err := BootstrapWithOptions(context.Background(), BootstrapOptions{
		ConfigPath: configPath,
		Config:     cfg,
		Force:      true,
	})
	if err != nil {
		t.Fatalf("BootstrapWithOptions() error = %v", err)
	}

	tlsDir := filepath.Join(dir, "tls")
	if _, err := os.Stat(filepath.Join(tlsDir, "server.pem")); err == nil {
		t.Fatalf("expected no server.pem for acme mode")
	}
}
