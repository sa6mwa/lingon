package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultConfigUsesConstants(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := DefaultConfig()

	if cfg.Server.Listen != DefaultListenAddr {
		t.Fatalf("Listen = %q, want %q", cfg.Server.Listen, DefaultListenAddr)
	}
	if cfg.Server.BasePath != DefaultBasePath {
		t.Fatalf("BasePath = %q, want %q", cfg.Server.BasePath, DefaultBasePath)
	}
	if cfg.Server.TLS.Mode != DefaultTLSMode {
		t.Fatalf("TLS.Mode = %q, want %q", cfg.Server.TLS.Mode, DefaultTLSMode)
	}

	expectedDir := filepath.Join(home, DefaultConfigDirName)
	if cfg.Server.DataDir != expectedDir {
		t.Fatalf("DataDir = %q, want %q", cfg.Server.DataDir, expectedDir)
	}

	expectedTLSDir := filepath.Join(expectedDir, DefaultTLSDirName)
	if cfg.Server.TLS.Dir != expectedTLSDir {
		t.Fatalf("TLS.Dir = %q, want %q", cfg.Server.TLS.Dir, expectedTLSDir)
	}

	expectedCache := filepath.Join(expectedTLSDir, DefaultTLSCacheDirName)
	if cfg.Server.TLS.CacheDir != expectedCache {
		t.Fatalf("TLS.CacheDir = %q, want %q", cfg.Server.TLS.CacheDir, expectedCache)
	}

	if cfg.Client.Endpoint != DefaultClientEndpoint {
		t.Fatalf("Client.Endpoint = %q, want %q", cfg.Client.Endpoint, DefaultClientEndpoint)
	}
	if cfg.Client.AuthFile != DefaultAuthPath() {
		t.Fatalf("Client.AuthFile = %q, want %q", cfg.Client.AuthFile, DefaultAuthPath())
	}
	if cfg.Client.LogFile != DefaultLogPath() {
		t.Fatalf("Client.LogFile = %q, want %q", cfg.Client.LogFile, DefaultLogPath())
	}
	if cfg.Client.BufferLines != DefaultBufferLines {
		t.Fatalf("Client.BufferLines = %d, want %d", cfg.Client.BufferLines, DefaultBufferLines)
	}
	if cfg.Terminal.Term != DefaultTerminalTerm {
		t.Fatalf("Terminal.Term = %q, want %q", cfg.Terminal.Term, DefaultTerminalTerm)
	}
}
