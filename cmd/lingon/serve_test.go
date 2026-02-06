package main

import (
	"testing"
	"time"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/testutil"
)

func TestServeNoBannerShorthand(t *testing.T) {
	t.Setenv("HOME", testutil.TempDir(t))

	loader := lingon.NewLoader()
	cmd := NewServeCommand(loader)
	if err := cmd.ParseFlags([]string{"-n"}); err != nil {
		t.Fatalf("ParseFlags error = %v", err)
	}

	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	if !cfg.Server.WebUI.NoBanner {
		t.Fatalf("Server.WebUI.NoBanner = %v, want true", cfg.Server.WebUI.NoBanner)
	}
}

func TestServeWallFlags(t *testing.T) {
	t.Setenv("HOME", testutil.TempDir(t))

	loader := lingon.NewLoader()
	cmd := NewServeCommand(loader)
	if err := cmd.ParseFlags([]string{"--wall-timeout", "7s", "--wall-inactive-after", "9m,12m"}); err != nil {
		t.Fatalf("ParseFlags error = %v", err)
	}

	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	if cfg.Server.Wall.Timeout != 7*time.Second {
		t.Fatalf("Server.Wall.Timeout = %v, want %v", cfg.Server.Wall.Timeout, 7*time.Second)
	}
	if cfg.Server.Wall.InactiveAfter != "9m,12m" {
		t.Fatalf("Server.Wall.InactiveAfter = %q, want %q", cfg.Server.Wall.InactiveAfter, "9m,12m")
	}
}

func TestServeWallInactiveAfterEmptyString(t *testing.T) {
	t.Setenv("HOME", testutil.TempDir(t))

	loader := lingon.NewLoader()
	cmd := NewServeCommand(loader)
	if err := cmd.ParseFlags([]string{"--wall-inactive-after", ""}); err != nil {
		t.Fatalf("ParseFlags error = %v", err)
	}

	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	if cfg.Server.Wall.InactiveAfter != "" {
		t.Fatalf("Server.Wall.InactiveAfter = %q, want empty", cfg.Server.Wall.InactiveAfter)
	}
}
