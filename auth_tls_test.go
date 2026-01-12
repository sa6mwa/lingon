package lingon

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/lingon/internal/tlsmgr"
)

func TestLoginUsesLocalCA(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	tlsDir := filepath.Join(home, ".lingon", "tls")

	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/login" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(loginResponse{
			AccessToken:      "access",
			AccessExpiresAt:  time.Now().Add(time.Minute),
			RefreshToken:     "refresh",
			RefreshExpiresAt: time.Now().Add(time.Hour),
		})
	})

	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	state, err := Login(context.Background(), LoginOptions{
		Endpoint: server.URL,
		Username: "user",
		Password: "pass",
		TOTP:     "123456",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if state.AccessToken == "" || state.RefreshToken == "" {
		t.Fatalf("expected tokens")
	}
}
