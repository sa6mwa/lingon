package cliwall

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/testutil"
)

func TestExecuteUsesExplicitEndpointAndAuthFile(t *testing.T) {
	configDir := testutil.SetLingonConfigEnv(t)

	now := time.Now().UTC()
	ambientAuthPath := filepath.Join(configDir, "auth.json")
	explicitAuthPath := filepath.Join(configDir, "explicit-auth.json")

	ambient := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected ambient endpoint hit: %s", r.URL)
	}))
	t.Cleanup(ambient.Close)

	var gotAuth string
	var gotPath string
	explicit := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"sent","sessions":1}`))
	}))
	t.Cleanup(explicit.Close)

	if err := lingon.SaveAuth(ambientAuthPath, lingon.AuthState{
		Endpoint:         ambient.URL + "/v1",
		AccessToken:      "ambient-token",
		AccessExpiresAt:  now.Add(5 * time.Minute),
		RefreshToken:     "ambient-refresh",
		RefreshExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("SaveAuth(ambient): %v", err)
	}
	if err := lingon.SaveAuth(explicitAuthPath, lingon.AuthState{
		Endpoint:         explicit.URL + "/v1",
		AccessToken:      "explicit-token",
		AccessExpiresAt:  now.Add(5 * time.Minute),
		RefreshToken:     "explicit-refresh",
		RefreshExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("SaveAuth(explicit): %v", err)
	}

	loader := lingon.NewLoader()
	var out bytes.Buffer
	err := Execute(t.Context(), Request{
		Loader:          loader,
		Endpoint:        explicit.URL + "/v1",
		EndpointChanged: true,
		AuthFile:        explicitAuthPath,
		AuthFileChanged: true,
		Quiet:           true,
		Insecure:        true,
		Message:         "hello",
		Stdout:          &out,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/v1/wall" {
		t.Fatalf("wall path = %q, want %q", gotPath, "/v1/wall")
	}
	if gotAuth != "Bearer explicit-token" {
		t.Fatalf("authorization = %q, want %q", gotAuth, "Bearer explicit-token")
	}
}
