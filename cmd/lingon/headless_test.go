package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
)

func TestHeadlessAliasEnabled(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "lingon", want: false},
		{name: "lingonx", want: true},
		{name: "/usr/local/bin/lingonx", want: true},
		{name: "/usr/local/bin/LINGONX", want: true},
	}
	for _, tc := range tests {
		if got := headlessAliasEnabled(tc.name); got != tc.want {
			t.Fatalf("headlessAliasEnabled(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestWithEnvReplacesExistingKey(t *testing.T) {
	env := []string{"A=1", "X=old", "B=2"}
	out := withEnv(env, "X", "new")
	found := false
	for _, e := range out {
		if e == "X=new" {
			found = true
		}
		if e == "X=old" {
			t.Fatalf("old key retained: %v", out)
		}
	}
	if !found {
		t.Fatalf("new key not found: %v", out)
	}
}

func TestResolveHeadlessSizeDefaults(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Int("cols", lingon.DefaultTerminalCols, "")
	cmd.Flags().Int("rows", lingon.DefaultTerminalRows, "")

	cols, rows, err := resolveHeadlessSize(cmd)
	if err != nil {
		t.Fatalf("resolveHeadlessSize: %v", err)
	}
	if cols != lingon.DefaultTerminalCols || rows != lingon.DefaultTerminalRows {
		t.Fatalf("resolveHeadlessSize defaults = %dx%d, want %dx%d", cols, rows, lingon.DefaultTerminalCols, lingon.DefaultTerminalRows)
	}
}

func TestResolveHeadlessSizeFlagOverrides(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Int("cols", lingon.DefaultTerminalCols, "")
	cmd.Flags().Int("rows", lingon.DefaultTerminalRows, "")
	if err := cmd.Flags().Set("cols", "132"); err != nil {
		t.Fatalf("set cols: %v", err)
	}
	if err := cmd.Flags().Set("rows", "41"); err != nil {
		t.Fatalf("set rows: %v", err)
	}

	cols, rows, err := resolveHeadlessSize(cmd)
	if err != nil {
		t.Fatalf("resolveHeadlessSize: %v", err)
	}
	if cols != 132 || rows != 41 {
		t.Fatalf("resolveHeadlessSize overrides = %dx%d, want 132x41", cols, rows)
	}
}

func TestFirstLocalHeadlessSessionUsesIDOrder(t *testing.T) {
	now := time.Now().UTC()
	selected, err := firstLocalHeadlessSession([]localHeadlessSession{
		{ID: "session-c", LastSeenAt: now},
		{ID: "session-a", LastSeenAt: now.Add(-time.Hour)},
		{ID: "session-b", LastSeenAt: now.Add(time.Hour)},
	})
	if err != nil {
		t.Fatalf("firstLocalHeadlessSession: %v", err)
	}
	if selected.ID != "session-a" {
		t.Fatalf("selected session=%q, want session-a", selected.ID)
	}
}

func TestResolveHeadlessRelayConfigPreservesExplicitOfflineRelay(t *testing.T) {
	cmd := headlessRelayTestCommand(t)
	if err := cmd.Flags().Set("offline", "true"); err != nil {
		t.Fatalf("set offline: %v", err)
	}
	if err := cmd.Flags().Set("endpoint", "https://relay.example/v1"); err != nil {
		t.Fatalf("set endpoint: %v", err)
	}
	if err := cmd.Flags().Set("token", "secret-token"); err != nil {
		t.Fatalf("set token: %v", err)
	}

	got, err := resolveHeadlessRelayConfig(cmd, lingon.NewLoader(), lingon.Config{}, false)
	if err != nil {
		t.Fatalf("resolveHeadlessRelayConfig: %v", err)
	}
	if got.Endpoint != "https://relay.example/v1" || got.Token != "secret-token" || !got.Offline {
		t.Fatalf("relay config = %+v, want offline relay endpoint/token preserved", got)
	}
	if got.AuthPath != "" {
		t.Fatalf("AuthPath = %q, want empty when --token is explicit", got.AuthPath)
	}
}

func TestResolveHeadlessRelayConfigDefaultOfflineNoAuthFallsBackLocalOnly(t *testing.T) {
	cmd := headlessRelayTestCommand(t)
	if err := cmd.Flags().Set("offline", "true"); err != nil {
		t.Fatalf("set offline: %v", err)
	}
	missingAuth := filepath.Join(t.TempDir(), "missing-auth.json")
	cfg := lingon.Config{}
	cfg.Client.AuthFile = missingAuth

	got, err := resolveHeadlessRelayConfig(cmd, lingon.NewLoader(), cfg, false)
	if err != nil {
		t.Fatalf("resolveHeadlessRelayConfig: %v", err)
	}
	if got.Endpoint != "" || got.Token != "" || got.AuthPath != "" || !got.Offline {
		t.Fatalf("relay config = %+v, want local-only offline fallback", got)
	}
}

func TestResolveHeadlessRelayConfigExplicitOfflineEndpointRequiresAuth(t *testing.T) {
	cmd := headlessRelayTestCommand(t)
	if err := cmd.Flags().Set("offline", "true"); err != nil {
		t.Fatalf("set offline: %v", err)
	}
	if err := cmd.Flags().Set("endpoint", "https://relay.example/v1"); err != nil {
		t.Fatalf("set endpoint: %v", err)
	}
	missingAuth := filepath.Join(t.TempDir(), "missing-auth.json")
	cfg := lingon.Config{}
	cfg.Client.AuthFile = missingAuth

	_, err := resolveHeadlessRelayConfig(cmd, lingon.NewLoader(), cfg, false)
	if err == nil {
		t.Fatalf("expected explicit offline endpoint without auth to fail")
	}
	if !strings.Contains(err.Error(), "auth file not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveHeadlessRelayConfigConfiguredOfflineEndpointRequiresAuth(t *testing.T) {
	cmd := headlessRelayTestCommand(t)
	if err := cmd.Flags().Set("offline", "true"); err != nil {
		t.Fatalf("set offline: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("client:\n  endpoint: https://configured.example/v1\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(config): %v", err)
	}
	loader := lingon.NewLoader()
	loader.SetConfigFile(configPath)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.Client.AuthFile = filepath.Join(t.TempDir(), "missing-auth.json")

	_, err = resolveHeadlessRelayConfig(cmd, loader, cfg, false)
	if err == nil {
		t.Fatalf("expected configured offline endpoint without auth to fail")
	}
	if !strings.Contains(err.Error(), "auth file not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveHeadlessRelayConfigEnvOfflineEndpointRequiresAuth(t *testing.T) {
	cmd := headlessRelayTestCommand(t)
	if err := cmd.Flags().Set("offline", "true"); err != nil {
		t.Fatalf("set offline: %v", err)
	}
	t.Setenv("LINGON_CLIENT_ENDPOINT", "https://env.example/v1")
	cfg := lingon.Config{}
	cfg.Client.Endpoint = "https://env.example/v1"
	cfg.Client.AuthFile = filepath.Join(t.TempDir(), "missing-auth.json")

	_, err := resolveHeadlessRelayConfig(cmd, lingon.NewLoader(), cfg, false)
	if err == nil {
		t.Fatalf("expected env offline endpoint without auth to fail")
	}
	if !strings.Contains(err.Error(), "auth file not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveHeadlessWallInactiveAfterLevelsUsesConfigDefault(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("wall-inactive-after", lingon.DefaultWallInactiveAfterCSV, "")
	cfg := lingon.Config{
		Terminal: lingon.TerminalConfig{
			WallInactiveAfter: "3m,7m",
		},
	}

	levels, err := resolveHeadlessWallInactiveAfterLevels(cmd, cfg)
	if err != nil {
		t.Fatalf("resolveHeadlessWallInactiveAfterLevels: %v", err)
	}
	want := []time.Duration{3 * time.Minute, 7 * time.Minute}
	if !reflect.DeepEqual(levels, want) {
		t.Fatalf("levels = %v, want %v", levels, want)
	}
}

func headlessRelayTestCommand(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Bool("offline", false, "")
	cmd.Flags().String("auth-file", lingon.DefaultAuthPath(), "")
	cmd.Flags().String("token", "", "")
	cmd.Flags().String("endpoint", lingon.DefaultClientEndpoint, "")
	return cmd
}

func TestResolveHeadlessWallInactiveAfterLevelsFlagOverridesConfig(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("wall-inactive-after", lingon.DefaultWallInactiveAfterCSV, "")
	if err := cmd.Flags().Set("wall-inactive-after", "1m,2m,4m"); err != nil {
		t.Fatalf("set wall-inactive-after: %v", err)
	}
	cfg := lingon.Config{
		Terminal: lingon.TerminalConfig{
			WallInactiveAfter: "3m,7m",
		},
	}

	levels, err := resolveHeadlessWallInactiveAfterLevels(cmd, cfg)
	if err != nil {
		t.Fatalf("resolveHeadlessWallInactiveAfterLevels: %v", err)
	}
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute}
	if !reflect.DeepEqual(levels, want) {
		t.Fatalf("levels = %v, want %v", levels, want)
	}
}

func TestResolveHeadlessWallInactiveAfterLevelsRejectsInvalid(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("wall-inactive-after", lingon.DefaultWallInactiveAfterCSV, "")
	if err := cmd.Flags().Set("wall-inactive-after", "nope"); err != nil {
		t.Fatalf("set wall-inactive-after: %v", err)
	}
	cfg := lingon.Config{}

	if _, err := resolveHeadlessWallInactiveAfterLevels(cmd, cfg); err == nil {
		t.Fatalf("expected parse error for invalid wall-inactive-after")
	}
}
