package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/testutil"
)

func TestWallCommandPrintsJSONResponseOnSuccess(t *testing.T) {
	home := testutil.TempDir(t)
	t.Setenv("HOME", home)

	authPath := filepath.Join(home, ".lingon", "auth.json")
	now := time.Now().UTC()

	var gotPath string
	var gotAuth string
	var gotBody string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll(body): %v", err)
		}
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"sent","sessions":2}`))
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
	cmd.SetArgs([]string{"wall", "--insecure", "hello", "world"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/v1/wall" {
		t.Fatalf("wall path = %q, want %q", gotPath, "/v1/wall")
	}
	if gotAuth != "Bearer access-token" {
		t.Fatalf("authorization header = %q, want %q", gotAuth, "Bearer access-token")
	}
	if gotBody != `{"message":"hello world"}` {
		t.Fatalf("request body = %q, want %q", gotBody, `{"message":"hello world"}`)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal(output): %v", err)
	}
	if got["status"] != "sent" || got["sessions"] != float64(2) {
		t.Fatalf("output json = %#v, want status=sent sessions=2", got)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", errOut.String())
	}
}

func TestWallCommandQuietSuppressesSuccessOutput(t *testing.T) {
	home := testutil.TempDir(t)
	t.Setenv("HOME", home)

	authPath := filepath.Join(home, ".lingon", "auth.json")
	now := time.Now().UTC()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"sent","sessions":1}`))
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
	cmd.SetArgs([]string{"wall", "--quiet", "--insecure", "hello"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no stdout output, got %q", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", errOut.String())
	}
}
