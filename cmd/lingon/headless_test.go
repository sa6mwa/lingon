package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/headlessd"
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

func TestHeadlessStartupReporterWritesReadyStatus(t *testing.T) {
	readFile, writeFile, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	defer func() {
		_ = readFile.Close()
	}()
	reporter := &headlessStartupReporter{file: writeFile}
	if err := reporter.Ready(headlessd.StartupReady{
		SessionID:  "session-a",
		SocketPath: "/tmp/lingon/headless/s.session-a.sock",
	}); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	reporter.Close()

	status, err := waitForHeadlessStartupStatus(readFile, nil, "")
	if err != nil {
		t.Fatalf("waitForHeadlessStartupStatus: %v", err)
	}
	if status.Status != "ready" || status.Session != "session-a" || status.Socket != "/tmp/lingon/headless/s.session-a.sock" {
		t.Fatalf("status = %+v, want ready session/socket", status)
	}
}

func TestHeadlessStartupReporterWritesFailureWithOfflineHint(t *testing.T) {
	readFile, writeFile, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	defer func() {
		_ = readFile.Close()
	}()
	reporter := &headlessStartupReporter{file: writeFile}
	if err := reporter.Failed(errors.New("auth file not found at /tmp/auth.json; run `lingon login -e https://relay.example/v1`")); err != nil {
		t.Fatalf("Failed: %v", err)
	}
	reporter.Close()

	status, err := waitForHeadlessStartupStatus(readFile, nil, "")
	if err != nil {
		t.Fatalf("waitForHeadlessStartupStatus: %v", err)
	}
	if status.Status != "error" {
		t.Fatalf("status = %+v, want error", status)
	}
	if !strings.Contains(status.Error, "auth file not found") || !strings.Contains(status.Error, "--offline") {
		t.Fatalf("error = %q, want auth failure with offline hint", status.Error)
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

func TestResolveHeadlessRelayConfigDefaultNoAuthRequiresOffline(t *testing.T) {
	cmd := headlessRelayTestCommand(t)
	missingAuth := filepath.Join(t.TempDir(), "missing-auth.json")
	cfg := lingon.Config{}
	cfg.Client.AuthFile = missingAuth

	_, err := resolveHeadlessRelayConfig(cmd, lingon.NewLoader(), cfg, false)
	if err == nil {
		t.Fatalf("expected default missing auth to require --offline")
	}
	if !strings.Contains(err.Error(), "auth file not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveHeadlessRelayConfigDefaultOfflineSingleStoredAuthPreservesRelay(t *testing.T) {
	cmd := headlessRelayTestCommand(t)
	if err := cmd.Flags().Set("offline", "true"); err != nil {
		t.Fatalf("set offline: %v", err)
	}
	authPath := filepath.Join(t.TempDir(), "auth.json")
	writeAuthEndpoints(t, authPath, "https://relay.example/v1")
	cfg := lingon.Config{}
	cfg.Client.AuthFile = authPath

	got, err := resolveHeadlessRelayConfig(cmd, lingon.NewLoader(), cfg, false)
	if err != nil {
		t.Fatalf("resolveHeadlessRelayConfig: %v", err)
	}
	if got.Endpoint != "https://relay.example/v1" || got.Token != "access-https://relay.example/v1" || got.AuthPath != authPath || !got.Offline {
		t.Fatalf("relay config = %+v, want offline stored relay auth preserved", got)
	}
}

func TestResolveHeadlessRelayConfigDefaultOfflineAmbiguousStoredAuthFallsBackLocalOnly(t *testing.T) {
	cmd := headlessRelayTestCommand(t)
	if err := cmd.Flags().Set("offline", "true"); err != nil {
		t.Fatalf("set offline: %v", err)
	}
	authPath := filepath.Join(t.TempDir(), "auth.json")
	writeAuthEndpoints(t, authPath, "https://alpha.example/v1", "https://beta.example/v1")
	cfg := lingon.Config{}
	cfg.Client.AuthFile = authPath

	got, err := resolveHeadlessRelayConfig(cmd, lingon.NewLoader(), cfg, false)
	if err != nil {
		t.Fatalf("resolveHeadlessRelayConfig: %v", err)
	}
	if got.Endpoint != "" || got.Token != "" || got.AuthPath != "" || !got.Offline {
		t.Fatalf("relay config = %+v, want local-only offline fallback", got)
	}
}

func TestResolveHeadlessRelayConfigExplicitOfflineAuthFileAmbiguityRequiresEndpoint(t *testing.T) {
	cmd := headlessRelayTestCommand(t)
	if err := cmd.Flags().Set("offline", "true"); err != nil {
		t.Fatalf("set offline: %v", err)
	}
	authPath := filepath.Join(t.TempDir(), "auth.json")
	writeAuthEndpoints(t, authPath, "https://alpha.example/v1", "https://beta.example/v1")
	if err := cmd.Flags().Set("auth-file", authPath); err != nil {
		t.Fatalf("set auth-file: %v", err)
	}

	_, err := resolveHeadlessRelayConfig(cmd, lingon.NewLoader(), lingon.Config{}, false)
	if err == nil {
		t.Fatalf("expected explicit auth file with ambiguous endpoints to fail")
	}
	if !strings.Contains(err.Error(), "endpoint is ambiguous") {
		t.Fatalf("unexpected error: %v", err)
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

func TestResolveHeadlessRelayConfigConfiguredOfflineEndpointNoAuthFallsBackLocalOnly(t *testing.T) {
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

	got, err := resolveHeadlessRelayConfig(cmd, loader, cfg, false)
	if err != nil {
		t.Fatalf("resolveHeadlessRelayConfig: %v", err)
	}
	if got.Endpoint != "" || got.Token != "" || got.AuthPath != "" || !got.Offline {
		t.Fatalf("relay config = %+v, want local-only offline fallback", got)
	}
}

func TestResolveHeadlessRelayConfigEnvOfflineEndpointNoAuthFallsBackLocalOnly(t *testing.T) {
	cmd := headlessRelayTestCommand(t)
	if err := cmd.Flags().Set("offline", "true"); err != nil {
		t.Fatalf("set offline: %v", err)
	}
	t.Setenv("LINGON_CLIENT_ENDPOINT", "https://env.example/v1")
	cfg := lingon.Config{}
	cfg.Client.Endpoint = "https://env.example/v1"
	cfg.Client.AuthFile = filepath.Join(t.TempDir(), "missing-auth.json")

	got, err := resolveHeadlessRelayConfig(cmd, lingon.NewLoader(), cfg, false)
	if err != nil {
		t.Fatalf("resolveHeadlessRelayConfig: %v", err)
	}
	if got.Endpoint != "" || got.Token != "" || got.AuthPath != "" || !got.Offline {
		t.Fatalf("relay config = %+v, want local-only offline fallback", got)
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

func writeAuthEndpoints(t *testing.T, authPath string, endpoints ...string) {
	t.Helper()
	now := time.Now().UTC()
	for _, endpoint := range endpoints {
		if err := lingon.SaveAuth(authPath, lingon.AuthState{
			Endpoint:         endpoint,
			AccessToken:      "access-" + endpoint,
			AccessExpiresAt:  now.Add(5 * time.Minute),
			RefreshToken:     "refresh-" + endpoint,
			RefreshExpiresAt: now.Add(time.Hour),
		}); err != nil {
			t.Fatalf("SaveAuth(%q): %v", endpoint, err)
		}
	}
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
