package lingon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/testutil"
)

func TestLoginAndRefreshSDK(t *testing.T) {
	store := relay.NewStore()
	users := relay.NewUserStore()
	user, err := relay.SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := relay.NewAuthenticator(users)
	server := relay.NewHTTPServer(store, users, auth, nil, nil)

	httptestServer := httptest.NewServer(server.Handler())
	t.Cleanup(httptestServer.Close)

	code, err := totp.GenerateCodeCustom(user.TOTPSecret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("GenerateCodeCustom: %v", err)
	}

	state, err := Login(context.Background(), LoginOptions{
		Endpoint: httptestServer.URL,
		Username: user.Username,
		Password: relay.DefaultTestPassword,
		TOTP:     code,
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if state.AccessToken == "" || state.RefreshToken == "" {
		t.Fatalf("expected tokens")
	}

	refreshed, err := Refresh(context.Background(), RefreshOptions{
		Endpoint:     httptestServer.URL,
		RefreshToken: state.RefreshToken,
	})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if refreshed.AccessToken == "" {
		t.Fatalf("expected access token")
	}
}

func TestRefreshSDKErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	if _, err := Refresh(context.Background(), RefreshOptions{
		Endpoint:     server.URL,
		RefreshToken: "bad",
	}); err == nil {
		t.Fatalf("expected error")
	}
}

func TestLogoutRemovesEndpointStateAndCallsRemote(t *testing.T) {
	dir := testutil.TempDir(t)
	authPath := filepath.Join(dir, "auth.json")
	now := time.Now().UTC()

	var gotPath string
	var gotAuth string
	var gotRefresh string
	logoutServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		var payload map[string]string
		_ = json.NewDecoder(r.Body).Decode(&payload)
		gotRefresh = payload["refresh_token"]
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(logoutServer.Close)

	stateA := AuthState{
		Endpoint:         logoutServer.URL + "/v1/",
		AccessToken:      "access-a",
		AccessExpiresAt:  now.Add(10 * time.Minute),
		RefreshToken:     "refresh-a",
		RefreshExpiresAt: now.Add(24 * time.Hour),
	}
	stateB := AuthState{
		Endpoint:         "https://other.example.com/v1",
		AccessToken:      "access-b",
		AccessExpiresAt:  now.Add(10 * time.Minute),
		RefreshToken:     "refresh-b",
		RefreshExpiresAt: now.Add(24 * time.Hour),
	}
	if err := SaveAuth(authPath, stateA); err != nil {
		t.Fatalf("SaveAuth A: %v", err)
	}
	if err := SaveAuth(authPath, stateB); err != nil {
		t.Fatalf("SaveAuth B: %v", err)
	}

	if err := Logout(context.Background(), LogoutOptions{
		Endpoint: stateA.Endpoint,
		AuthFile: authPath,
	}); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	if gotPath != "/v1/auth/logout" {
		t.Fatalf("logout path = %q, want %q", gotPath, "/v1/auth/logout")
	}
	if gotAuth != "Bearer "+stateA.AccessToken {
		t.Fatalf("logout auth header = %q, want %q", gotAuth, "Bearer "+stateA.AccessToken)
	}
	if gotRefresh != stateA.RefreshToken {
		t.Fatalf("logout refresh token = %q, want %q", gotRefresh, stateA.RefreshToken)
	}

	if _, err := LoadAuthForEndpoint(authPath, stateA.Endpoint); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("LoadAuthForEndpoint A err = %v, want os.ErrNotExist", err)
	}
	if _, err := LoadAuthForEndpoint(authPath, stateB.Endpoint); err != nil {
		t.Fatalf("LoadAuthForEndpoint B: %v", err)
	}
}

func TestLogoutRemoteFailureStillRemovesLocalState(t *testing.T) {
	dir := testutil.TempDir(t)
	authPath := filepath.Join(dir, "auth.json")
	now := time.Now().UTC()

	logoutServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(logoutServer.Close)

	state := AuthState{
		Endpoint:         logoutServer.URL + "/v1",
		AccessToken:      "access",
		AccessExpiresAt:  now.Add(10 * time.Minute),
		RefreshToken:     "refresh",
		RefreshExpiresAt: now.Add(24 * time.Hour),
	}
	if err := SaveAuth(authPath, state); err != nil {
		t.Fatalf("SaveAuth: %v", err)
	}

	err := Logout(context.Background(), LogoutOptions{
		Endpoint: state.Endpoint,
		AuthFile: authPath,
	})
	if err == nil {
		t.Fatalf("Logout expected remote error")
	}
	if _, loadErr := LoadAuthForEndpoint(authPath, state.Endpoint); !errors.Is(loadErr, os.ErrNotExist) {
		t.Fatalf("LoadAuthForEndpoint err = %v, want os.ErrNotExist", loadErr)
	}
}

func TestLogoutIsIdempotentWhenAuthMissing(t *testing.T) {
	path := filepath.Join(testutil.TempDir(t), "auth.json")
	if err := Logout(context.Background(), LogoutOptions{
		Endpoint: "https://example.com/v1",
		AuthFile: path,
	}); err != nil {
		t.Fatalf("Logout: %v", err)
	}
}

func TestLoadAuthErrorsWhenMultipleEndpointsStored(t *testing.T) {
	path := filepath.Join(testutil.TempDir(t), "auth.json")
	now := time.Now().UTC()

	if err := SaveAuth(path, AuthState{
		Endpoint:         "https://one.example.com/v1",
		AccessToken:      "a1",
		AccessExpiresAt:  now.Add(time.Minute),
		RefreshToken:     "r1",
		RefreshExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("SaveAuth first: %v", err)
	}
	if err := SaveAuth(path, AuthState{
		Endpoint:         "https://two.example.com/v1",
		AccessToken:      "a2",
		AccessExpiresAt:  now.Add(time.Minute),
		RefreshToken:     "r2",
		RefreshExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("SaveAuth second: %v", err)
	}

	if _, err := LoadAuth(path); err == nil {
		t.Fatalf("LoadAuth expected error for multiple endpoints")
	}
}
