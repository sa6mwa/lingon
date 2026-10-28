//go:build integration

package integrationwebui_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebUIAuthNavigationReportsLoadErrors(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "lingon_access", Value: "test-access"})
			http.SetCookie(w, &http.Cookie{Name: "lingon_refresh", Value: "test-refresh"})
		case "/v1/auth/share":
			http.SetCookie(w, &http.Cookie{Name: "bifrons_share_session", Value: "test-share"})
		default:
			// An actual browser load error, without a timer or server sleep.
			http.Redirect(w, r, "/v1/", http.StatusFound)
		}
	}))
	defer server.Close()
	for _, test := range []struct {
		name string
		open func(context.Context, string) error
	}{
		{"login", func(ctx context.Context, url string) error {
			return ensureWebUILogin(ctx, url, "user", "pass", "123456")
		}},
		{"share", func(ctx context.Context, url string) error { return ensureWebUIShare(ctx, url, "token") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := newChromedpContext(t, t.TempDir())
			defer cancel()
			if err := test.open(ctx, server.URL+"/v1/"); err == nil {
				t.Fatal("authentication helper ignored the browser document load failure")
			}
		})
	}
}
