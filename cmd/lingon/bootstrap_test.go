package main

import (
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v3"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/testutil"
)

func TestParseSetValues(t *testing.T) {
	values := []string{
		`server.listen=":1234"`,
		"terminal.respawn=true",
		"server.base=",
		"server.tls.dir=/app/certs",
	}

	got, err := parseSetValues(values)
	if err != nil {
		t.Fatalf("parseSetValues error = %v", err)
	}

	if got["server.listen"] != ":1234" {
		t.Fatalf("server.listen = %#v, want :1234", got["server.listen"])
	}
	if got["terminal.respawn"] != true {
		t.Fatalf("terminal.respawn = %#v, want true", got["terminal.respawn"])
	}
	if got["server.base"] != "" {
		t.Fatalf("server.base = %#v, want empty string", got["server.base"])
	}
	if got["server.tls.dir"] != "/app/certs" {
		t.Fatalf("server.tls.dir = %#v, want /app/certs", got["server.tls.dir"])
	}
}

func TestBootstrapCLIShorthands(t *testing.T) {
	testutil.SetLingonConfigEnv(t)
	dir := testutil.TempDir(t)
	configPath := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(configPath, []byte("server:\n  base: /original\n"), 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	loader := lingon.NewLoader()
	cmd := NewRootCommand(loader)
	cmd.SetArgs([]string{
		"bootstrap",
		"-c", configPath,
		"-f",
		"-s", "server.base=/lingon",
		"-s", "server.tls.mode=acme",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute error = %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}

	var cfg lingon.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal error = %v", err)
	}
	if cfg.Server.BasePath != "/lingon" {
		t.Fatalf("Server.BasePath = %q, want /lingon", cfg.Server.BasePath)
	}
	if cfg.Server.TLS.Mode != "acme" {
		t.Fatalf("Server.TLS.Mode = %q, want acme", cfg.Server.TLS.Mode)
	}
}
