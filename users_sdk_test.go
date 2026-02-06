package lingon

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"pkt.systems/lingon/internal/relay"
)

func TestUsersSDKFlow(t *testing.T) {
	store := relay.NewStore()
	users := relay.NewUserStore()
	admin, err := relay.SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := relay.NewAuthenticator(users)
	server := relay.NewHTTPServer(store, users, auth, nil, nil)

	access, err := store.CreateAccessToken(admin.Username, relay.DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	httptestServer := httptest.NewServer(server.Handler())
	t.Cleanup(httptestServer.Close)

	createResp, err := UsersAdd(context.Background(), UserCreateOptions{
		Endpoint:    httptestServer.URL,
		AccessToken: access.Token,
		Username:    "alice",
	})
	if err != nil {
		t.Fatalf("UsersAdd: %v", err)
	}
	if createResp.Password == "" || createResp.TOTPSecret == "" || createResp.TOTPURL == "" {
		t.Fatalf("expected user creation details")
	}

	list, err := UsersList(context.Background(), httptestServer.URL, access.Token)
	if err != nil {
		t.Fatalf("UsersList: %v", err)
	}
	if len(list) == 0 {
		t.Fatalf("expected user list")
	}

	rotated, err := UsersRotateTOTP(context.Background(), httptestServer.URL, access.Token, "alice")
	if err != nil {
		t.Fatalf("UsersRotateTOTP: %v", err)
	}
	if rotated.TOTPSecret == "" || rotated.TOTPURL == "" {
		t.Fatalf("expected totp details")
	}

	passwd, err := UsersChpasswd(context.Background(), httptestServer.URL, access.Token, "alice", "")
	if err != nil {
		t.Fatalf("UsersChpasswd: %v", err)
	}
	if passwd.Password == "" {
		t.Fatalf("expected password response")
	}

	if err := UsersDelete(context.Background(), httptestServer.URL, access.Token, "alice"); err != nil {
		t.Fatalf("UsersDelete: %v", err)
	}
}
