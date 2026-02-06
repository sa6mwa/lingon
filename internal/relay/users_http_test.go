package relay

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUsersLifecycle(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	admin, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)

	now := time.Now().UTC()
	access, err := store.CreateAccessToken(admin.Username, DefaultAccessTokenTTL, now)
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	addBody, _ := json.Marshal(userCreateRequest{Username: "alice"})
	addReq := httptest.NewRequest(http.MethodPost, "/users", bytes.NewReader(addBody))
	addReq.Header.Set("Authorization", "Bearer "+access.Token)
	addResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(addResp, addReq)
	if addResp.Code != http.StatusOK {
		t.Fatalf("add status = %d, want %d", addResp.Code, http.StatusOK)
	}
	var created userCreateResponse
	if err := json.NewDecoder(addResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode add response: %v", err)
	}
	if created.Password == "" {
		t.Fatalf("expected generated password")
	}
	if created.TOTPSecret == "" || created.TOTPURL == "" {
		t.Fatalf("expected totp details")
	}
	alice, ok := users.Get("alice")
	if !ok {
		t.Fatalf("user not stored")
	}

	listReq := httptest.NewRequest(http.MethodGet, "/users", nil)
	listReq.Header.Set("Authorization", "Bearer "+access.Token)
	listResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listResp.Code, http.StatusOK)
	}
	var listed []userResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	found := false
	for _, user := range listed {
		if user.Username == "alice" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected alice in list")
	}

	rotateReq := httptest.NewRequest(http.MethodPost, "/users/alice/rotate-totp", nil)
	rotateReq.Header.Set("Authorization", "Bearer "+access.Token)
	rotateResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(rotateResp, rotateReq)
	if rotateResp.Code != http.StatusOK {
		t.Fatalf("rotate status = %d, want %d", rotateResp.Code, http.StatusOK)
	}
	var rotated userTOTPResponse
	if err := json.NewDecoder(rotateResp.Body).Decode(&rotated); err != nil {
		t.Fatalf("decode rotate response: %v", err)
	}
	if rotated.TOTPSecret == "" || rotated.TOTPURL == "" {
		t.Fatalf("expected rotated totp")
	}
	if rotated.TOTPSecret == created.TOTPSecret {
		t.Fatalf("totp secret did not change")
	}
	alice, ok = users.Get("alice")
	if !ok {
		t.Fatalf("user missing after rotate")
	}
	if alice.TOTPSecret != rotated.TOTPSecret {
		t.Fatalf("totp secret not updated in store")
	}

	chpasswdBody, _ := json.Marshal(userPasswordRequest{})
	chpasswdReq := httptest.NewRequest(http.MethodPost, "/users/alice/password", bytes.NewReader(chpasswdBody))
	chpasswdReq.Header.Set("Authorization", "Bearer "+access.Token)
	chpasswdResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(chpasswdResp, chpasswdReq)
	if chpasswdResp.Code != http.StatusOK {
		t.Fatalf("chpasswd status = %d, want %d", chpasswdResp.Code, http.StatusOK)
	}
	var passwordResp userPasswordResponse
	if err := json.NewDecoder(chpasswdResp.Body).Decode(&passwordResp); err != nil {
		t.Fatalf("decode chpasswd response: %v", err)
	}
	if passwordResp.Password == "" {
		t.Fatalf("expected generated password")
	}
	alice, ok = users.Get("alice")
	if !ok {
		t.Fatalf("user missing after chpasswd")
	}
	if alice.PasswordHash == passwordResp.Password {
		t.Fatalf("password should be hashed in store")
	}

	aliceAccess, err := store.CreateAccessToken(alice.Username, DefaultAccessTokenTTL, now)
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}
	aliceRefresh, err := store.CreateRefreshToken(alice.Username, DefaultRefreshTokenTTL, now)
	if err != nil {
		t.Fatalf("CreateRefreshToken: %v", err)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/users/alice", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+access.Token)
	deleteResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteResp, deleteReq)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d", deleteResp.Code, http.StatusOK)
	}
	if _, ok := users.Get("alice"); ok {
		t.Fatalf("user still present after delete")
	}
	if _, err := store.ValidateAccessToken(aliceAccess.Token, now); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("expected access token revoked, got %v", err)
	}
	if _, err := store.ValidateRefreshToken(aliceRefresh.Token, now); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("expected refresh token revoked, got %v", err)
	}
}
