package config

import (
	"path/filepath"
	"testing"

	"pkt.systems/lingon/internal/testutil"
)

func TestDefaultConfigUsesConstants(t *testing.T) {
	home := testutil.TempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("TERM", "xterm-256color")

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
	if cfg.Server.ConnectLimit.Disable != DefaultConnectLimitDisable {
		t.Fatalf("ConnectLimit.Disable = %v, want %v", cfg.Server.ConnectLimit.Disable, DefaultConnectLimitDisable)
	}
	if cfg.Server.ConnectLimit.Burst != DefaultConnectLimitBurst {
		t.Fatalf("ConnectLimit.Burst = %d, want %d", cfg.Server.ConnectLimit.Burst, DefaultConnectLimitBurst)
	}
	if cfg.Server.ConnectLimit.Count != DefaultConnectLimitCount {
		t.Fatalf("ConnectLimit.Count = %d, want %d", cfg.Server.ConnectLimit.Count, DefaultConnectLimitCount)
	}
	if cfg.Server.ConnectLimit.Window != DefaultConnectLimitWindow {
		t.Fatalf("ConnectLimit.Window = %v, want %v", cfg.Server.ConnectLimit.Window, DefaultConnectLimitWindow)
	}
	if cfg.Server.WebUI.NoBanner != DefaultWebUINoBanner {
		t.Fatalf("WebUI.NoBanner = %v, want %v", cfg.Server.WebUI.NoBanner, DefaultWebUINoBanner)
	}
	if cfg.Server.Wall.Timeout != DefaultWallTimeout {
		t.Fatalf("Wall.Timeout = %v, want %v", cfg.Server.Wall.Timeout, DefaultWallTimeout)
	}
	if cfg.Server.Wall.InactiveAfter != DefaultWallInactiveAfterCSV {
		t.Fatalf("Wall.InactiveAfter = %q, want %q", cfg.Server.Wall.InactiveAfter, DefaultWallInactiveAfterCSV)
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
	if cfg.Terminal.ScrollbackLines != DefaultScrollbackLines {
		t.Fatalf("Terminal.ScrollbackLines = %d, want %d", cfg.Terminal.ScrollbackLines, DefaultScrollbackLines)
	}
	if cfg.Terminal.Term != "xterm-256color" {
		t.Fatalf("Terminal.Term = %q, want %q", cfg.Terminal.Term, "xterm-256color")
	}
	if cfg.Terminal.Respawn != DefaultTerminalRespawn {
		t.Fatalf("Terminal.Respawn = %v, want %v", cfg.Terminal.Respawn, DefaultTerminalRespawn)
	}
	if cfg.Terminal.Theme != DefaultTerminalTheme {
		t.Fatalf("Terminal.Theme = %q, want %q", cfg.Terminal.Theme, DefaultTerminalTheme)
	}
	if cfg.Terminal.HostnameOnly != DefaultTerminalHostnameOnly {
		t.Fatalf("Terminal.HostnameOnly = %v, want %v", cfg.Terminal.HostnameOnly, DefaultTerminalHostnameOnly)
	}
	if cfg.Terminal.WallInactiveAfter != DefaultWallInactiveAfterCSV {
		t.Fatalf("Terminal.WallInactiveAfter = %q, want %q", cfg.Terminal.WallInactiveAfter, DefaultWallInactiveAfterCSV)
	}
}

func TestConfigForDirUsesProvidedDir(t *testing.T) {
	dir := filepath.Join(testutil.TempDir(t), "cfg")

	cfg := ForDir(dir)

	if cfg.Server.DataDir != dir {
		t.Fatalf("DataDir = %q, want %q", cfg.Server.DataDir, dir)
	}
	if cfg.Server.UsersFile != filepath.Join(dir, DefaultUsersFileName) {
		t.Fatalf("UsersFile = %q, want %q", cfg.Server.UsersFile, filepath.Join(dir, DefaultUsersFileName))
	}
	if cfg.Server.TLS.Dir != filepath.Join(dir, DefaultTLSDirName) {
		t.Fatalf("TLS.Dir = %q, want %q", cfg.Server.TLS.Dir, filepath.Join(dir, DefaultTLSDirName))
	}
	if cfg.Server.TLS.CacheDir != filepath.Join(dir, DefaultTLSDirName, DefaultTLSCacheDirName) {
		t.Fatalf("TLS.CacheDir = %q, want %q", cfg.Server.TLS.CacheDir, filepath.Join(dir, DefaultTLSDirName, DefaultTLSCacheDirName))
	}
	if cfg.Client.AuthFile != filepath.Join(dir, DefaultAuthFileName) {
		t.Fatalf("Client.AuthFile = %q, want %q", cfg.Client.AuthFile, filepath.Join(dir, DefaultAuthFileName))
	}
	if cfg.Client.LogFile != "" {
		t.Fatalf("Client.LogFile = %q, want empty", cfg.Client.LogFile)
	}
}
