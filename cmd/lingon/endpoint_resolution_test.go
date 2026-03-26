package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/testutil"
)

func TestSessionsCommandInfersEndpointFromSingleStoredAuthWhenUnset(t *testing.T) {
	home := testutil.TempDir(t)
	t.Setenv("HOME", home)

	authPath := filepath.Join(home, ".lingon", "auth.json")
	now := time.Now().UTC()

	var gotPath string
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(server.Close)

	if err := lingon.SaveAuth(authPath, lingon.AuthState{
		Endpoint:         server.URL + "/v1",
		AccessToken:      "access-token",
		AccessExpiresAt:  now.Add(5 * time.Minute),
		RefreshToken:     "refresh-token",
		RefreshExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("SaveAuth: %v", err)
	}

	loader := lingon.NewLoader()
	cmd := NewRootCommand(loader)

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"sessions"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/v1/sessions" {
		t.Fatalf("sessions path = %q, want %q", gotPath, "/v1/sessions")
	}
	if gotAuth != "Bearer access-token" {
		t.Fatalf("authorization header = %q, want %q", gotAuth, "Bearer access-token")
	}
	if strings.TrimSpace(out.String()) != "[]" {
		t.Fatalf("output = %q, want []", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", errOut.String())
	}
}

func TestSessionsCommandErrorsWhenStoredEndpointsAreAmbiguous(t *testing.T) {
	home := testutil.TempDir(t)
	t.Setenv("HOME", home)

	authPath := filepath.Join(home, ".lingon", "auth.json")
	now := time.Now().UTC()

	for _, endpoint := range []string{
		"https://alpha.example.com/v1",
		"https://beta.example.com/v1",
	} {
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

	loader := lingon.NewLoader()
	cmd := NewRootCommand(loader)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"sessions"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected ambiguity error")
	}
	if !strings.Contains(err.Error(), "endpoint is ambiguous") {
		t.Fatalf("error = %v, want endpoint ambiguity", err)
	}
	if !strings.Contains(err.Error(), "https://alpha.example.com/v1") {
		t.Fatalf("error = %v, want alpha endpoint listed", err)
	}
	if !strings.Contains(err.Error(), "https://beta.example.com/v1") {
		t.Fatalf("error = %v, want beta endpoint listed", err)
	}
}

func TestResolveEndpointValuePrefersConfiguredEndpointOverStoredAuth(t *testing.T) {
	home := testutil.TempDir(t)
	t.Setenv("HOME", home)

	authPath := filepath.Join(home, ".lingon", "auth.json")
	now := time.Now().UTC()
	if err := lingon.SaveAuth(authPath, lingon.AuthState{
		Endpoint:         "https://stored.example.com/v1",
		AccessToken:      "access-token",
		AccessExpiresAt:  now.Add(5 * time.Minute),
		RefreshToken:     "refresh-token",
		RefreshExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("SaveAuth: %v", err)
	}

	configPath := filepath.Join(home, ".lingon", "config.yaml")
	if err := os.WriteFile(configPath, []byte("client:\n  endpoint: https://configured.example.com/v1\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(config): %v", err)
	}

	loader := lingon.NewLoader()
	loader.Viper().SetDefault("client.endpoint", lingon.DefaultClientEndpoint)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("endpoint", lingon.DefaultClientEndpoint, "")

	endpointValue, err := resolveEndpointValue(cmd, loader, cfg.Client.Endpoint, lingon.DefaultClientEndpoint, authPath)
	if err != nil {
		t.Fatalf("resolveEndpointValue: %v", err)
	}
	if endpointValue != "https://configured.example.com/v1" {
		t.Fatalf("endpointValue = %q, want %q", endpointValue, "https://configured.example.com/v1")
	}
}

func TestResolveEndpointValueFallsBackToLocalhostWithoutStoredAuth(t *testing.T) {
	loader := lingon.NewLoader()
	loader.Viper().SetDefault("client.endpoint", lingon.DefaultClientEndpoint)

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("endpoint", lingon.DefaultClientEndpoint, "")

	endpointValue, err := resolveEndpointValue(cmd, loader, lingon.DefaultClientEndpoint, lingon.DefaultClientEndpoint, filepath.Join(testutil.TempDir(t), "missing-auth.json"))
	if err != nil {
		t.Fatalf("resolveEndpointValue: %v", err)
	}
	if endpointValue != lingon.DefaultClientEndpoint {
		t.Fatalf("endpointValue = %q, want %q", endpointValue, lingon.DefaultClientEndpoint)
	}
}

func TestResolveEndpointValueIgnoresStoredAuthForExplicitTokenFlag(t *testing.T) {
	home := testutil.TempDir(t)
	t.Setenv("HOME", home)

	authPath := filepath.Join(home, ".lingon", "auth.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(auth dir): %v", err)
	}
	if err := os.WriteFile(authPath, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile(auth): %v", err)
	}

	loader := lingon.NewLoader()
	loader.Viper().SetDefault("client.endpoint", lingon.DefaultClientEndpoint)

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("endpoint", lingon.DefaultClientEndpoint, "")
	cmd.Flags().String("token", "", "")
	if err := cmd.Flags().Set("token", "explicit-token"); err != nil {
		t.Fatalf("Set(token): %v", err)
	}

	endpointValue, err := resolveEndpointValue(cmd, loader, lingon.DefaultClientEndpoint, lingon.DefaultClientEndpoint, authPath)
	if err != nil {
		t.Fatalf("resolveEndpointValue: %v", err)
	}
	if endpointValue != lingon.DefaultClientEndpoint {
		t.Fatalf("endpointValue = %q, want %q", endpointValue, lingon.DefaultClientEndpoint)
	}
}

func TestResolveEndpointValueIgnoresBrokenAuthForLogin(t *testing.T) {
	home := testutil.TempDir(t)
	t.Setenv("HOME", home)

	authPath := filepath.Join(home, ".lingon", "auth.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(auth dir): %v", err)
	}
	if err := os.WriteFile(authPath, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile(auth): %v", err)
	}

	loader := lingon.NewLoader()
	loader.Viper().SetDefault("client.endpoint", lingon.DefaultClientEndpoint)

	cmd := &cobra.Command{Use: "login"}
	cmd.Flags().String("endpoint", lingon.DefaultClientEndpoint, "")

	endpointValue, err := resolveEndpointValue(cmd, loader, lingon.DefaultClientEndpoint, lingon.DefaultClientEndpoint, authPath)
	if err != nil {
		t.Fatalf("resolveEndpointValue: %v", err)
	}
	if endpointValue != lingon.DefaultClientEndpoint {
		t.Fatalf("endpointValue = %q, want %q", endpointValue, lingon.DefaultClientEndpoint)
	}
}

func TestSessionsCommandIgnoresStoredAuthForExplicitAccessToken(t *testing.T) {
	home := testutil.TempDir(t)
	t.Setenv("HOME", home)

	authPath := filepath.Join(home, ".lingon", "auth.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(auth dir): %v", err)
	}
	if err := os.WriteFile(authPath, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile(auth): %v", err)
	}

	var gotPath string
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(server.Close)

	configPath := filepath.Join(home, ".lingon", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(config dir): %v", err)
	}
	configBody := []byte("client:\n  endpoint: " + server.URL + "/v1\n")
	if err := os.WriteFile(configPath, configBody, 0o600); err != nil {
		t.Fatalf("WriteFile(config): %v", err)
	}

	loader := lingon.NewLoader()
	cmd := NewRootCommand(loader)

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"sessions", "--access-token", "explicit-access-token"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/v1/sessions" {
		t.Fatalf("sessions path = %q, want %q", gotPath, "/v1/sessions")
	}
	if gotAuth != "Bearer explicit-access-token" {
		t.Fatalf("authorization header = %q, want %q", gotAuth, "Bearer explicit-access-token")
	}
	if strings.TrimSpace(out.String()) != "[]" {
		t.Fatalf("output = %q, want []", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", errOut.String())
	}
}
