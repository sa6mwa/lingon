package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/testutil"
)

func TestLogoutCommandRemovesEndpointAuth(t *testing.T) {
	home := testutil.TempDir(t)
	t.Setenv("HOME", home)
	authPath := filepath.Join(home, ".lingon", "auth.json")
	now := time.Now().UTC()

	var called bool
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	if err := lingon.SaveAuth(authPath, lingon.AuthState{
		Endpoint:         server.URL + "/v1/",
		AccessToken:      "access",
		AccessExpiresAt:  now.Add(5 * time.Minute),
		RefreshToken:     "refresh",
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
	cmd.SetArgs([]string{"logout", "-e", server.URL + "/v1", "--auth-file", authPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !called {
		t.Fatalf("expected remote logout call")
	}
	if gotPath != "/v1/auth/logout" {
		t.Fatalf("logout path = %q, want %q", gotPath, "/v1/auth/logout")
	}
	if _, err := lingon.LoadAuthForEndpoint(authPath, server.URL+"/v1"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("LoadAuthForEndpoint err = %v, want os.ErrNotExist", err)
	}
}
