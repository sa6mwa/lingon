package relay

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func TestLoginFlow(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)

	code, err := totp.GenerateCodeCustom(user.TOTPSecret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("GenerateCodeCustom: %v", err)
	}

	payload, _ := json.Marshal(loginRequest{Username: user.Username, Password: DefaultTestPassword, TOTP: code})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(payload))
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	var out loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.AccessToken == "" || out.RefreshToken == "" {
		t.Fatalf("expected tokens")
	}
	if out.AccessExpiresAt.Before(time.Now().UTC()) {
		t.Fatalf("access token should be in the future")
	}
	if out.RefreshExpiresAt.Before(time.Now().UTC()) {
		t.Fatalf("refresh token should be in the future")
	}
}

func TestRefreshFlow(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)

	refresh, err := store.CreateRefreshToken(user.Username, DefaultRefreshTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateRefreshToken: %v", err)
	}

	payload, _ := json.Marshal(refreshRequest{RefreshToken: refresh.Token})
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader(payload))
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	var out loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.AccessToken == "" {
		t.Fatalf("expected access token")
	}
}

func TestShareTokenEndpoints(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)

	session := Session{ID: "s1", Username: user.Username, CreatedAt: time.Now().UTC(), Status: "active"}
	store.CreateSession(session)

	body, _ := json.Marshal(shareCreateRequest{SessionID: session.ID, Scope: string(ShareScopeView), TTL: "1h"})
	req := httptest.NewRequest(http.MethodPost, "/share/create", bytes.NewReader(body))
	access, err := store.CreateAccessToken(user.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+access.Token)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}

	var created shareCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if created.Token == "" {
		t.Fatalf("expected token")
	}

	revokeReq := shareRevokeRequest(created)
	revokeBody, _ := json.Marshal(revokeReq)
	req = httptest.NewRequest(http.MethodPost, "/share/revoke", bytes.NewReader(revokeBody))
	req.Header.Set("Authorization", "Bearer "+access.Token)
	resp = httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
}

func TestListSessionsRequiresAuth(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
}
