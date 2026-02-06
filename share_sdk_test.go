package lingon

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"pkt.systems/lingon/internal/relay"
)

func TestShareCreateAndRevoke(t *testing.T) {
	store := relay.NewStore()
	users := relay.NewUserStore()
	user, err := relay.SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := relay.NewAuthenticator(users)
	server := relay.NewHTTPServer(store, users, auth, nil, nil)

	session := relay.Session{
		ID:           "s1",
		Username:     user.Username,
		CreatedAt:    time.Now().UTC(),
		LastActiveAt: time.Now().UTC(),
		Status:       "active",
	}
	store.CreateSession(session)

	access, err := store.CreateAccessToken(user.Username, relay.DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	httptestServer := httptest.NewServer(server.Handler())
	t.Cleanup(httptestServer.Close)

	createResp, err := ShareCreate(context.Background(), ShareCreateOptions{
		Endpoint:    httptestServer.URL,
		AccessToken: access.Token,
		SessionID:   session.ID,
		Scope:       ShareScopeView,
		TTL:         time.Hour,
	})
	if err != nil {
		t.Fatalf("ShareCreate: %v", err)
	}
	if createResp.Token == "" {
		t.Fatalf("expected share token")
	}

	revokeResp, err := ShareRevoke(context.Background(), ShareRevokeOptions{
		Endpoint:    httptestServer.URL,
		AccessToken: access.Token,
		Token:       createResp.Token,
	})
	if err != nil {
		t.Fatalf("ShareRevoke: %v", err)
	}
	if revokeResp.Status == "" {
		t.Fatalf("expected status")
	}
}
