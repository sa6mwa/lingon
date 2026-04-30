package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"pkt.systems/lingon/internal/protocolpb"
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

	req = httptest.NewRequest(http.MethodGet, "/share/list", nil)
	req.Header.Set("Authorization", "Bearer "+access.Token)
	resp = httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	var listed []ShareToken
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("expected 0 share token, got %d", len(listed))
	}

	req = httptest.NewRequest(http.MethodPost, "/share/revoke-all", bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", "Bearer "+access.Token)
	resp = httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
}

func TestShareTokenEndpointsEnforceOwnership(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	userAResult, err := CreateUser(users, "alice", "pass-a", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateUser A: %v", err)
	}
	userBResult, err := CreateUser(users, "bob", "pass-b", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateUser B: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)

	sessionA := Session{ID: "s1", Username: userAResult.User.Username, CreatedAt: time.Now().UTC(), Status: "active"}
	sessionB := Session{ID: "s2", Username: userBResult.User.Username, CreatedAt: time.Now().UTC(), Status: "active"}
	store.CreateSession(sessionA)
	store.CreateSession(sessionB)

	accessA, err := store.CreateAccessToken(userAResult.User.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken A: %v", err)
	}
	accessB, err := store.CreateAccessToken(userBResult.User.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken B: %v", err)
	}

	body, _ := json.Marshal(shareCreateRequest{SessionID: sessionB.ID, Scope: string(ShareScopeView), TTL: "1h"})
	req := httptest.NewRequest(http.MethodPost, "/share/create", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessA.Token)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusNotFound)
	}

	body, _ = json.Marshal(shareCreateRequest{SessionID: sessionA.ID, Scope: string(ShareScopeView), TTL: "1h"})
	req = httptest.NewRequest(http.MethodPost, "/share/create", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+accessA.Token)
	resp = httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	var created shareCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	revokeReq := shareRevokeRequest(created)
	revokeBody, _ := json.Marshal(revokeReq)
	req = httptest.NewRequest(http.MethodPost, "/share/revoke", bytes.NewReader(revokeBody))
	req.Header.Set("Authorization", "Bearer "+accessB.Token)
	resp = httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusNotFound)
	}

	req = httptest.NewRequest(http.MethodGet, "/share/list", nil)
	req.Header.Set("Authorization", "Bearer "+accessB.Token)
	resp = httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	var listed []ShareToken
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("expected 0 share tokens, got %d", len(listed))
	}
}

func TestShareListFiltersByStatus(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)

	now := time.Now().UTC()
	session := Session{ID: "s1", Username: user.Username, CreatedAt: now, Status: "active"}
	store.CreateSession(session)

	access, err := store.CreateAccessToken(user.Username, DefaultAccessTokenTTL, now)
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	valid, err := store.CreateShareToken(session.ID, ShareScopeView, time.Hour, now)
	if err != nil {
		t.Fatalf("CreateShareToken valid: %v", err)
	}
	revoked, err := store.CreateShareToken(session.ID, ShareScopeView, time.Hour, now)
	if err != nil {
		t.Fatalf("CreateShareToken revoked: %v", err)
	}
	if err := store.RevokeShareToken(revoked.Token, now); err != nil {
		t.Fatalf("RevokeShareToken: %v", err)
	}
	expired, err := store.CreateShareToken(session.ID, ShareScopeView, time.Hour, now.Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("CreateShareToken expired: %v", err)
	}

	cases := []struct {
		name string
		url  string
		want int
	}{
		{name: "default", url: "/share/list", want: 1},
		{name: "valid", url: "/share/list?status=valid", want: 1},
		{name: "revoked", url: "/share/list?status=revoked", want: 1},
		{name: "expired", url: "/share/list?status=expired", want: 1},
		{name: "valid+revoked", url: "/share/list?status=valid&status=revoked", want: 2},
		{name: "valid+expired", url: "/share/list?status=valid&status=expired", want: 2},
		{name: "revoked+expired", url: "/share/list?status=revoked&status=expired", want: 2},
		{name: "all", url: "/share/list?status=valid&status=revoked&status=expired", want: 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			req.Header.Set("Authorization", "Bearer "+access.Token)
			resp := httptest.NewRecorder()
			server.Handler().ServeHTTP(resp, req)
			if resp.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
			}
			var listed []ShareToken
			if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if len(listed) != tc.want {
				t.Fatalf("expected %d share tokens, got %d", tc.want, len(listed))
			}
		})
	}

	if valid.Token == "" || revoked.Token == "" || expired.Token == "" {
		t.Fatalf("expected share tokens")
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

func TestListSessionsScopesToUser(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	userA, err := CreateUser(users, "alice", "pass-a", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateUser A: %v", err)
	}
	userB, err := CreateUser(users, "bob", "pass-b", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateUser B: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)

	store.CreateSession(Session{ID: "alice-session", Username: userA.User.Username, CreatedAt: time.Now().UTC(), Status: "active"})
	store.CreateSession(Session{ID: "bob-session", Username: userB.User.Username, CreatedAt: time.Now().UTC(), Status: "active"})

	if got := store.ListSessions(userA.User.Username); len(got) == 0 {
		t.Fatalf("expected alice sessions in store")
	}
	if got := store.ListSessions(userB.User.Username); len(got) == 0 {
		t.Fatalf("expected bob sessions in store")
	}

	accessA, err := store.CreateAccessToken(userA.User.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken A: %v", err)
	}
	accessB, err := store.CreateAccessToken(userB.User.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken B: %v", err)
	}

	if err := server.Hub.RegisterHost(&fakeConn{id: "host-a", role: RoleHost, sessionID: "alice-session", scope: ShareScopeControl}, "alice-session", 80, 24); err != nil {
		t.Fatalf("RegisterHost alice: %v", err)
	}
	if err := server.Hub.RegisterHost(&fakeConn{id: "host-b", role: RoleHost, sessionID: "bob-session", scope: ShareScopeControl}, "bob-session", 80, 24); err != nil {
		t.Fatalf("RegisterHost bob: %v", err)
	}

	if sessions, _ := server.listActiveSessions(userA.User.Username, time.Now().UTC()); len(sessions) == 0 {
		t.Fatalf("expected listActiveSessions to return alice sessions; store=%+v", store.ListSessions(userA.User.Username))
	}

	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+accessA.Token)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	var sessionsA []Session
	if err := json.NewDecoder(resp.Body).Decode(&sessionsA); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(sessionsA) != 1 || sessionsA[0].ID != "alice-session" {
		t.Fatalf("unexpected sessions for alice: %+v", sessionsA)
	}

	req = httptest.NewRequest(http.MethodGet, "/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+accessB.Token)
	resp = httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	var sessionsB []Session
	if err := json.NewDecoder(resp.Body).Decode(&sessionsB); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(sessionsB) != 1 || sessionsB[0].ID != "bob-session" {
		t.Fatalf("unexpected sessions for bob: %+v", sessionsB)
	}
}

func TestShareSessionCookieCannotAccessAuthenticatedAPIs(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)

	now := time.Now().UTC()
	store.CreateSession(Session{
		ID:           "share-session",
		Username:     user.Username,
		Name:         "Shared host",
		CreatedAt:    now,
		LastActiveAt: now,
		Status:       "active",
	})
	share, err := store.CreateShareToken("share-session", ShareScopeView, time.Hour, now)
	if err != nil {
		t.Fatalf("CreateShareToken: %v", err)
	}

	shareBody, _ := json.Marshal(shareAuthRequest{Token: share.Token})
	shareReq := httptest.NewRequest(http.MethodPost, "/auth/share", bytes.NewReader(shareBody))
	shareResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(shareResp, shareReq)
	if shareResp.Code != http.StatusOK {
		t.Fatalf("share auth status = %d, want %d", shareResp.Code, http.StatusOK)
	}

	var shareCookie *http.Cookie
	for _, cookie := range shareResp.Result().Cookies() {
		if cookie.Name == shareCookieName {
			shareCookie = cookie
			break
		}
	}
	if shareCookie == nil || shareCookie.Value == "" {
		t.Fatalf("expected share session cookie")
	}

	// Share session cookie can restore share attach state.
	sessionReq := httptest.NewRequest(http.MethodGet, "/auth/share/session", nil)
	sessionReq.AddCookie(shareCookie)
	sessionResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(sessionResp, sessionReq)
	if sessionResp.Code != http.StatusOK {
		t.Fatalf("share session status = %d, want %d", sessionResp.Code, http.StatusOK)
	}

	tests := []struct {
		method string
		path   string
		body   []byte
	}{
		{method: http.MethodGet, path: "/sessions"},
		{method: http.MethodGet, path: "/share/list"},
		{method: http.MethodPost, path: "/share/create", body: []byte(`{"session_id":"share-session","scope":"view"}`)},
		{method: http.MethodPost, path: "/share/revoke-all", body: []byte(`{}`)},
		{method: http.MethodPost, path: "/wall", body: []byte(`{"message":"hello"}`)},
		{method: http.MethodGet, path: "/wall/events"},
		{method: http.MethodPost, path: "/wall/inactivity", body: []byte(`{"session_id":"share-session","enabled":true}`)},
	}
	for _, tc := range tests {
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewReader(tc.body))
		req.AddCookie(shareCookie)
		if len(tc.body) > 0 {
			req.Header.Set("Content-Type", "application/json")
		}
		resp := httptest.NewRecorder()
		server.Handler().ServeHTTP(resp, req)
		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d, want %d", tc.method, tc.path, resp.Code, http.StatusUnauthorized)
		}
	}
}

func TestLogoutClearsShareSessionCookie(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)

	now := time.Now().UTC()
	store.CreateSession(Session{
		ID:           "share-session",
		Username:     user.Username,
		CreatedAt:    now,
		LastActiveAt: now,
		Status:       "active",
	})
	share, err := store.CreateShareToken("share-session", ShareScopeView, time.Hour, now)
	if err != nil {
		t.Fatalf("CreateShareToken: %v", err)
	}

	shareBody, _ := json.Marshal(shareAuthRequest{Token: share.Token})
	shareReq := httptest.NewRequest(http.MethodPost, "/auth/share", bytes.NewReader(shareBody))
	shareResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(shareResp, shareReq)
	if shareResp.Code != http.StatusOK {
		t.Fatalf("share auth status = %d, want %d", shareResp.Code, http.StatusOK)
	}
	var shareCookie *http.Cookie
	for _, cookie := range shareResp.Result().Cookies() {
		if cookie.Name == shareCookieName {
			shareCookie = cookie
			break
		}
	}
	if shareCookie == nil {
		t.Fatalf("expected share session cookie")
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/auth/logout", bytes.NewReader([]byte("{}")))
	logoutReq.AddCookie(shareCookie)
	logoutResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(logoutResp, logoutReq)
	if logoutResp.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want %d", logoutResp.Code, http.StatusOK)
	}

	var cleared bool
	for _, cookie := range logoutResp.Result().Cookies() {
		if cookie.Name == shareCookieName && cookie.MaxAge < 0 {
			cleared = true
			break
		}
	}
	if !cleared {
		t.Fatalf("expected cleared share session cookie in logout response")
	}

	// The old share session cookie is no longer accepted after logout.
	sessionReq := httptest.NewRequest(http.MethodGet, "/auth/share/session", nil)
	sessionReq.AddCookie(shareCookie)
	sessionResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(sessionResp, sessionReq)
	if sessionResp.Code != http.StatusUnauthorized {
		t.Fatalf("share session status = %d, want %d", sessionResp.Code, http.StatusUnauthorized)
	}
}

func TestWallSendDispatchesToActiveParticipantSessions(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)

	now := time.Now().UTC()
	store.CreateSession(Session{ID: "s-active", Username: user.Username, CreatedAt: now, LastActiveAt: now, Status: "active"})
	store.CreateSession(Session{ID: "s-idle", Username: user.Username, CreatedAt: now, LastActiveAt: now, Status: "active"})

	host := &fakeConn{id: "host-active", role: RoleHost, sessionID: "s-active", scope: ShareScopeControl}
	if err := server.Hub.RegisterHost(host, "s-active", 80, 24); err != nil {
		t.Fatalf("RegisterHost: %v", err)
	}

	access, err := store.CreateAccessToken(user.Username, DefaultAccessTokenTTL, now)
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	body, _ := json.Marshal(wallRequest{Message: "hello world"})
	req := httptest.NewRequest(http.MethodPost, "/wall", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+access.Token)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	var out wallResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Sessions != 1 {
		t.Fatalf("Sessions = %d, want 1", out.Sessions)
	}
	if len(host.sent) != 1 {
		t.Fatalf("host sent frames = %d, want 1", len(host.sent))
	}
	wall := host.sent[0].GetWall()
	if wall == nil {
		t.Fatalf("expected wall frame")
	}
	if wall.Message != "hello world" {
		t.Fatalf("wall.Message = %q, want %q", wall.Message, "hello world")
	}
	if wall.Sender == "" {
		t.Fatalf("expected non-empty wall sender")
	}
}

func TestWallInactivityEndpoint(t *testing.T) {
	boolRef := func(v bool) *bool { return &v }

	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)

	now := time.Now().UTC()
	store.CreateSession(Session{ID: "s1", Username: user.Username, CreatedAt: now, LastActiveAt: now, Status: "active"})
	access, err := store.CreateAccessToken(user.Username, DefaultAccessTokenTTL, now)
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	enableBody, _ := json.Marshal(wallInactivityRequest{SessionID: "s1", Enabled: boolRef(true)})
	enableReq := httptest.NewRequest(http.MethodPost, "/wall/inactivity", bytes.NewReader(enableBody))
	enableReq.Header.Set("Authorization", "Bearer "+access.Token)
	enableResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(enableResp, enableReq)
	if enableResp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", enableResp.Code, http.StatusOK)
	}
	var enabled wallInactivityResponse
	if err := json.NewDecoder(enableResp.Body).Decode(&enabled); err != nil {
		t.Fatalf("decode enabled: %v", err)
	}
	if !enabled.Enabled {
		t.Fatalf("Enabled = %v, want true", enabled.Enabled)
	}
	if enabled.InactiveAfter != "2m" {
		t.Fatalf("InactiveAfter = %q, want %q", enabled.InactiveAfter, "2m")
	}

	disableBody, _ := json.Marshal(wallInactivityRequest{SessionID: "s1", Enabled: boolRef(false)})
	disableReq := httptest.NewRequest(http.MethodPost, "/wall/inactivity", bytes.NewReader(disableBody))
	disableReq.Header.Set("Authorization", "Bearer "+access.Token)
	disableResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(disableResp, disableReq)
	if disableResp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", disableResp.Code, http.StatusOK)
	}
	var disabled wallInactivityResponse
	if err := json.NewDecoder(disableResp.Body).Decode(&disabled); err != nil {
		t.Fatalf("decode disabled: %v", err)
	}
	if disabled.Enabled {
		t.Fatalf("Enabled = %v, want false", disabled.Enabled)
	}
	if disabled.InactiveAfter != "" {
		t.Fatalf("InactiveAfter = %q, want empty", disabled.InactiveAfter)
	}

	toggleBody, _ := json.Marshal(wallInactivityRequest{SessionID: "s1"})
	toggleReq := httptest.NewRequest(http.MethodPost, "/wall/inactivity", bytes.NewReader(toggleBody))
	toggleReq.Header.Set("Authorization", "Bearer "+access.Token)
	toggleResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(toggleResp, toggleReq)
	if toggleResp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", toggleResp.Code, http.StatusOK)
	}
	var toggledOn wallInactivityResponse
	if err := json.NewDecoder(toggleResp.Body).Decode(&toggledOn); err != nil {
		t.Fatalf("decode toggledOn: %v", err)
	}
	if !toggledOn.Enabled {
		t.Fatalf("Enabled = %v, want true", toggledOn.Enabled)
	}
	if toggledOn.InactiveAfter != "2m" {
		t.Fatalf("InactiveAfter = %q, want %q", toggledOn.InactiveAfter, "2m")
	}

	toggleReq2 := httptest.NewRequest(http.MethodPost, "/wall/inactivity", bytes.NewReader(toggleBody))
	toggleReq2.Header.Set("Authorization", "Bearer "+access.Token)
	toggleResp2 := httptest.NewRecorder()
	server.Handler().ServeHTTP(toggleResp2, toggleReq2)
	if toggleResp2.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", toggleResp2.Code, http.StatusOK)
	}
	var toggledOff wallInactivityResponse
	if err := json.NewDecoder(toggleResp2.Body).Decode(&toggledOff); err != nil {
		t.Fatalf("decode toggledOff: %v", err)
	}
	if !toggledOff.Enabled {
		t.Fatalf("Enabled = %v, want true", toggledOff.Enabled)
	}
	if toggledOff.InactiveAfter != "5m" {
		t.Fatalf("InactiveAfter = %q, want %q", toggledOff.InactiveAfter, "5m")
	}

	toggleReq3 := httptest.NewRequest(http.MethodPost, "/wall/inactivity", bytes.NewReader(toggleBody))
	toggleReq3.Header.Set("Authorization", "Bearer "+access.Token)
	toggleResp3 := httptest.NewRecorder()
	server.Handler().ServeHTTP(toggleResp3, toggleReq3)
	if toggleResp3.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", toggleResp3.Code, http.StatusOK)
	}
	var toggledThird wallInactivityResponse
	if err := json.NewDecoder(toggleResp3.Body).Decode(&toggledThird); err != nil {
		t.Fatalf("decode toggledThird: %v", err)
	}
	if !toggledThird.Enabled {
		t.Fatalf("Enabled = %v, want true", toggledThird.Enabled)
	}
	if toggledThird.InactiveAfter != "15m" {
		t.Fatalf("InactiveAfter = %q, want %q", toggledThird.InactiveAfter, "15m")
	}

	toggleReq4 := httptest.NewRequest(http.MethodPost, "/wall/inactivity", bytes.NewReader(toggleBody))
	toggleReq4.Header.Set("Authorization", "Bearer "+access.Token)
	toggleResp4 := httptest.NewRecorder()
	server.Handler().ServeHTTP(toggleResp4, toggleReq4)
	if toggleResp4.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", toggleResp4.Code, http.StatusOK)
	}
	var toggledFourth wallInactivityResponse
	if err := json.NewDecoder(toggleResp4.Body).Decode(&toggledFourth); err != nil {
		t.Fatalf("decode toggledFourth: %v", err)
	}
	if toggledFourth.Enabled {
		t.Fatalf("Enabled = %v, want false", toggledFourth.Enabled)
	}
	if toggledFourth.InactiveAfter != "" {
		t.Fatalf("InactiveAfter = %q, want empty", toggledFourth.InactiveAfter)
	}
}

func TestWallEventsEndpoint(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	alice, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	bobUsername := "bob"
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)

	now := time.Now().UTC()
	store.CreateSession(Session{ID: "a1", Username: alice.Username, CreatedAt: now, LastActiveAt: now, Status: "active"})
	store.CreateSession(Session{ID: "b1", Username: bobUsername, CreatedAt: now, LastActiveAt: now, Status: "active"})

	svc := server.wallService()
	if svc == nil {
		t.Fatalf("wall service unavailable")
	}
	if _, err := svc.sendUserWall(alice.Username, "alice@127.0.0.1", "first", now); err != nil {
		t.Fatalf("sendUserWall first: %v", err)
	}
	if _, err := svc.sendUserWall(alice.Username, "alice@127.0.0.1", "second", now.Add(time.Second)); err != nil {
		t.Fatalf("sendUserWall second: %v", err)
	}
	if _, err := svc.sendUserWall(bobUsername, "bob@127.0.0.1", "hidden", now.Add(2*time.Second)); err != nil {
		t.Fatalf("sendUserWall bob: %v", err)
	}

	access, err := store.CreateAccessToken(alice.Username, DefaultAccessTokenTTL, now)
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	firstReq := httptest.NewRequest(http.MethodGet, "/wall/events?since=0&limit=1", nil)
	firstReq.Header.Set("Authorization", "Bearer "+access.Token)
	firstResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(firstResp, firstReq)
	if firstResp.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d", firstResp.Code, http.StatusOK)
	}
	var first wallEventsResponse
	if err := json.NewDecoder(firstResp.Body).Decode(&first); err != nil {
		t.Fatalf("decode first: %v", err)
	}
	if len(first.Events) != 1 {
		t.Fatalf("first events = %d, want 1", len(first.Events))
	}
	if first.Events[0].Message != "first" {
		t.Fatalf("first message = %q, want %q", first.Events[0].Message, "first")
	}
	if !first.HasMore {
		t.Fatalf("first HasMore = false, want true")
	}
	if first.NextID == 0 {
		t.Fatalf("expected non-zero next id")
	}

	secondReq := httptest.NewRequest(http.MethodGet, "/wall/events?since="+fmt.Sprintf("%d", first.NextID)+"&limit=10", nil)
	secondReq.Header.Set("Authorization", "Bearer "+access.Token)
	secondResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(secondResp, secondReq)
	if secondResp.Code != http.StatusOK {
		t.Fatalf("second status = %d, want %d", secondResp.Code, http.StatusOK)
	}
	var second wallEventsResponse
	if err := json.NewDecoder(secondResp.Body).Decode(&second); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	if len(second.Events) != 1 {
		t.Fatalf("second events = %d, want 1", len(second.Events))
	}
	if second.Events[0].Message != "second" {
		t.Fatalf("second message = %q, want %q", second.Events[0].Message, "second")
	}
	if second.HasMore {
		t.Fatalf("second HasMore = true, want false")
	}
}

func TestWallEventsEndpointSkipsStaleInactivityForInactiveSourceSession(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	alice, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)

	now := time.Now().UTC()
	store.CreateSession(Session{ID: "a1", Username: alice.Username, CreatedAt: now, LastActiveAt: now, Status: "active"})

	svc := server.wallService()
	if svc == nil {
		t.Fatalf("wall service unavailable")
	}
	_, err = svc.sendUserWallForSession(
		alice.Username,
		"alice@127.0.0.1",
		"bash inactive",
		"a1",
		protocolpb.WallKind_WALL_KIND_INACTIVITY,
		now,
	)
	if err != nil {
		t.Fatalf("sendUserWallForSession inactivity: %v", err)
	}
	if changed := store.MarkSessionInactive("a1", "", now.Add(time.Second)); !changed {
		t.Fatalf("MarkSessionInactive = false, want true")
	}

	access, err := store.CreateAccessToken(alice.Username, DefaultAccessTokenTTL, now)
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/wall/events?since=0&limit=10", nil)
	req.Header.Set("Authorization", "Bearer "+access.Token)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	var out wallEventsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out.Events) != 0 {
		t.Fatalf("events = %d, want 0", len(out.Events))
	}
	if out.NextID == 0 {
		t.Fatalf("NextID = 0, want skipped event cursor advancement")
	}
	if out.HasMore {
		t.Fatalf("HasMore = true, want false")
	}
}

func TestWallEventsEndpointKeepsManualWallForInactiveSourceSession(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	alice, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)

	now := time.Now().UTC()
	store.CreateSession(Session{ID: "a1", Username: alice.Username, CreatedAt: now, LastActiveAt: now, Status: "active"})

	svc := server.wallService()
	if svc == nil {
		t.Fatalf("wall service unavailable")
	}
	_, err = svc.sendUserWallForSession(
		alice.Username,
		"alice@127.0.0.1",
		"manual wall",
		"a1",
		protocolpb.WallKind_WALL_KIND_UNSPECIFIED,
		now,
	)
	if err != nil {
		t.Fatalf("sendUserWallForSession manual: %v", err)
	}
	if changed := store.MarkSessionInactive("a1", "", now.Add(time.Second)); !changed {
		t.Fatalf("MarkSessionInactive = false, want true")
	}

	access, err := store.CreateAccessToken(alice.Username, DefaultAccessTokenTTL, now)
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/wall/events?since=0&limit=10", nil)
	req.Header.Set("Authorization", "Bearer "+access.Token)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	var out wallEventsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(out.Events))
	}
	if out.Events[0].Message != "manual wall" {
		t.Fatalf("message = %q, want %q", out.Events[0].Message, "manual wall")
	}
}

func TestWallEventsEndpointKeepsActiveInactivity(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	alice, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)

	now := time.Now().UTC()
	store.CreateSession(Session{ID: "a1", Username: alice.Username, CreatedAt: now, LastActiveAt: now, Status: "active"})

	svc := server.wallService()
	if svc == nil {
		t.Fatalf("wall service unavailable")
	}
	_, err = svc.sendUserWallForSession(
		alice.Username,
		"alice@127.0.0.1",
		"bash inactive",
		"a1",
		protocolpb.WallKind_WALL_KIND_INACTIVITY,
		now,
	)
	if err != nil {
		t.Fatalf("sendUserWallForSession inactivity: %v", err)
	}

	access, err := store.CreateAccessToken(alice.Username, DefaultAccessTokenTTL, now)
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/wall/events?since=0&limit=10", nil)
	req.Header.Set("Authorization", "Bearer "+access.Token)
	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	var out wallEventsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(out.Events))
	}
	if got := out.Events[0].Kind; got != uint32(protocolpb.WallKind_WALL_KIND_INACTIVITY) {
		t.Fatalf("Kind = %d, want %d", got, protocolpb.WallKind_WALL_KIND_INACTIVITY)
	}
}
