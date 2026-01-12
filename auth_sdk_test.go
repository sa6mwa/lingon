package lingon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"pkt.systems/lingon/internal/relay"
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
