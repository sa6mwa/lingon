package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
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
	setLocalUserRequest(addReq)
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
	setLocalUserRequest(listReq)
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
	setLocalUserRequest(rotateReq)
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
	setLocalUserRequest(chpasswdReq)
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
	store.CreateSession(Session{ID: "alice-session", Username: alice.Username, CreatedAt: now, LastActiveAt: now, Status: "active"})
	aliceShare, err := store.CreateShareToken("alice-session", ShareScopeView, time.Hour, now)
	if err != nil {
		t.Fatalf("CreateShareToken: %v", err)
	}
	host := &fakeConn{id: "alice-host", role: RoleHost, sessionID: "alice-session", scope: ShareScopeControl}
	if err := server.Hub.RegisterHost(host, "alice-session", 80, 24); err != nil {
		t.Fatalf("RegisterHost: %v", err)
	}
	shareClient := &fakeConn{
		id:         "alice-share-client",
		role:       RoleClient,
		sessionID:  "alice-session",
		scope:      ShareScopeView,
		shareToken: aliceShare.Token,
	}
	server.Hub.RegisterClient(shareClient, "alice-session", "share-client", false)

	deleteReq := httptest.NewRequest(http.MethodDelete, "/users/alice", nil)
	setLocalUserRequest(deleteReq)
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
	if share, ok := store.GetShareToken(aliceShare.Token); !ok || share.RevokedAt == nil {
		t.Fatalf("expected alice share token revoked, got %+v ok=%v", share, ok)
	}
	if got := server.Hub.ClientCount("alice-session"); got != 0 {
		t.Fatalf("share clients still connected = %d, want 0", got)
	}
	if host.closed != 1 {
		t.Fatalf("host closed %d times, want 1", host.closed)
	}
	if shareClient.closed != 1 {
		t.Fatalf("share client closed %d times, want 1", shareClient.closed)
	}
}

func TestUserCredentialRotationRevokesTokensAndDisconnectsSessions(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		body   []byte
		assert func(t *testing.T, users *UserStore, resp *httptest.ResponseRecorder)
	}{
		{
			name: "password",
			path: "/users/alice/password",
			body: mustMarshalUserPasswordRequest(t, userPasswordRequest{}),
			assert: func(t *testing.T, users *UserStore, resp *httptest.ResponseRecorder) {
				t.Helper()
				var passwordResp userPasswordResponse
				if err := json.NewDecoder(resp.Body).Decode(&passwordResp); err != nil {
					t.Fatalf("decode password response: %v", err)
				}
				if passwordResp.Password == "" {
					t.Fatalf("expected generated password")
				}
			},
		},
		{
			name: "totp",
			path: "/users/alice/rotate-totp",
			assert: func(t *testing.T, users *UserStore, resp *httptest.ResponseRecorder) {
				t.Helper()
				var totpResp userTOTPResponse
				if err := json.NewDecoder(resp.Body).Decode(&totpResp); err != nil {
					t.Fatalf("decode totp response: %v", err)
				}
				if totpResp.TOTPSecret == "" || totpResp.TOTPURL == "" {
					t.Fatalf("expected rotated totp")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := NewStore()
			users := NewUserStore()
			admin, err := SeedTestUser(users)
			if err != nil {
				t.Fatalf("SeedTestUser: %v", err)
			}
			now := time.Now().UTC()
			aliceResult, err := CreateUser(users, "alice", "", now)
			if err != nil {
				t.Fatalf("CreateUser: %v", err)
			}
			auth := NewAuthenticator(users)
			server := NewHTTPServer(store, users, auth, nil, nil)
			adminAccess, err := store.CreateAccessToken(admin.Username, DefaultAccessTokenTTL, now)
			if err != nil {
				t.Fatalf("CreateAccessToken admin: %v", err)
			}
			aliceAccess, err := store.CreateAccessToken(aliceResult.User.Username, DefaultAccessTokenTTL, now)
			if err != nil {
				t.Fatalf("CreateAccessToken alice: %v", err)
			}
			aliceRefresh, err := store.CreateRefreshToken(aliceResult.User.Username, DefaultRefreshTokenTTL, now)
			if err != nil {
				t.Fatalf("CreateRefreshToken alice: %v", err)
			}
			store.CreateSession(Session{ID: "alice-session", Username: aliceResult.User.Username, CreatedAt: now, LastActiveAt: now, Status: "active"})
			host := &fakeConn{id: "alice-host", role: RoleHost, sessionID: "alice-session", scope: ShareScopeControl}
			client := &fakeConn{id: "alice-client", role: RoleClient, sessionID: "alice-session", scope: ShareScopeControl}
			if err := server.Hub.RegisterHost(host, "alice-session", 80, 24); err != nil {
				t.Fatalf("RegisterHost: %v", err)
			}
			server.Hub.RegisterClient(client, "alice-session", "alice-client", false)

			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader(tc.body))
			setLocalUserRequest(req)
			req.Header.Set("Authorization", "Bearer "+adminAccess.Token)
			resp := httptest.NewRecorder()
			server.Handler().ServeHTTP(resp, req)
			if resp.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
			}
			tc.assert(t, users, resp)
			if _, err := store.ValidateAccessToken(aliceAccess.Token, now); !errors.Is(err, ErrTokenNotFound) {
				t.Fatalf("expected access token revoked, got %v", err)
			}
			if _, err := store.ValidateRefreshToken(aliceRefresh.Token, now); !errors.Is(err, ErrTokenNotFound) {
				t.Fatalf("expected refresh token revoked, got %v", err)
			}
			if host.closed != 1 {
				t.Fatalf("host closed %d times, want 1", host.closed)
			}
			if client.closed != 1 {
				t.Fatalf("client closed %d times, want 1", client.closed)
			}
			if got := server.Hub.ClientCount("alice-session"); got != 0 {
				t.Fatalf("clients still connected = %d, want 0", got)
			}
		})
	}
}

func TestUsersEndpointsRejectRemoteAuthenticatedRequests(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	admin, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)
	access, err := store.CreateAccessToken(admin.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	req.RemoteAddr = "203.0.113.10:4321"
	req.Header.Set("Authorization", "Bearer "+access.Token)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusForbidden)
	}
}

func TestUsersEndpointsRejectLoopbackProxyForRemoteClient(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	admin, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)
	access, err := store.CreateAccessToken(admin.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	req.RemoteAddr = "127.0.0.1:4321"
	req.Host = "127.0.0.1"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	req.Header.Set("Authorization", "Bearer "+access.Token)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusForbidden)
	}
}

func TestUsersEndpointsRejectSpoofedLoopbackForwardedClient(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	admin, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)
	access, err := store.CreateAccessToken(admin.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	req.RemoteAddr = "127.0.0.1:4321"
	req.Host = "localhost"
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.Header.Set("Authorization", "Bearer "+access.Token)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusForbidden)
	}
}

func TestUsersEndpointsRejectHeaderlessLoopbackProxyHost(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	admin, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)
	access, err := store.CreateAccessToken(admin.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	req.RemoteAddr = "127.0.0.1:4321"
	req.Host = "relay.example.com"
	req.Header.Set("Authorization", "Bearer "+access.Token)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusForbidden)
	}
}

func TestUsersEndpointsRejectHeaderlessLoopbackProxyWithLoopbackListener(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	admin, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)
	access, err := store.CreateAccessToken(admin.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	req.RemoteAddr = "127.0.0.1:4321"
	req.Host = "relay.example.com"
	req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}))
	req.Header.Set("Authorization", "Bearer "+access.Token)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusForbidden)
	}
}

func TestUsersEndpointsRejectLoopbackProxyOnPublicListener(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	admin, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)
	access, err := store.CreateAccessToken(admin.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	req.RemoteAddr = "127.0.0.1:4321"
	req.Host = "localhost"
	req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("203.0.113.20"), Port: 443}))
	req.Header.Set("Authorization", "Bearer "+access.Token)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusForbidden)
	}
}

func TestUserActionDecodesEscapedUsername(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	admin, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)
	access, err := store.CreateAccessToken(admin.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}
	if _, err := CreateUser(users, "john doe", "password", time.Now().UTC()); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	rotateReq := httptest.NewRequest(http.MethodPost, "/users/john%20doe/rotate-totp", nil)
	rotateReq.URL.Path = "/users/john doe/rotate-totp"
	rotateReq.URL.RawPath = "/users/john%20doe/rotate-totp"
	setLocalUserRequest(rotateReq)
	rotateReq.Header.Set("Authorization", "Bearer "+access.Token)
	rotateResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(rotateResp, rotateReq)
	if rotateResp.Code != http.StatusOK {
		t.Fatalf("rotate status = %d, want %d body=%s", rotateResp.Code, http.StatusOK, rotateResp.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/users/john%20doe", nil)
	deleteReq.URL.Path = "/users/john doe"
	deleteReq.URL.RawPath = "/users/john%20doe"
	setLocalUserRequest(deleteReq)
	deleteReq.Header.Set("Authorization", "Bearer "+access.Token)
	deleteResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteResp, deleteReq)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d body=%s", deleteResp.Code, http.StatusOK, deleteResp.Body.String())
	}
	if _, ok := users.Get("john doe"); ok {
		t.Fatalf("escaped delete did not remove decoded username")
	}
}

func TestCreateUserRejectsPathSeparators(t *testing.T) {
	users := NewUserStore()
	for _, username := range []string{"alice/bob", `alice\bob`} {
		if _, err := CreateUser(users, username, "password", time.Now().UTC()); !errors.Is(err, ErrUsernameInvalid) {
			t.Fatalf("CreateUser(%q) err = %v, want %v", username, err, ErrUsernameInvalid)
		}
	}
}

func TestCreateUserConcurrentDuplicateReturnsOneSuccess(t *testing.T) {
	users := NewUserStore()
	const attempts = 8
	errs := make(chan error, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := CreateUser(users, "race-user", "password", time.Now().UTC())
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	successes := 0
	conflicts := 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrUserExists):
			conflicts++
		default:
			t.Fatalf("CreateUser err = %v, want nil or %v", err, ErrUserExists)
		}
	}
	if successes != 1 || conflicts != attempts-1 {
		t.Fatalf("successes=%d conflicts=%d, want 1/%d", successes, conflicts, attempts-1)
	}
}

func setLocalUserRequest(req *http.Request) {
	req.RemoteAddr = "127.0.0.1:12345"
	req.Host = "127.0.0.1"
	ctx := context.WithValue(req.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12843})
	*req = *req.WithContext(ctx)
}

func mustMarshalUserPasswordRequest(t *testing.T, req userPasswordRequest) []byte {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal password request: %v", err)
	}
	return body
}
