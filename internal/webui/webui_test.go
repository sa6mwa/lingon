package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerDefaultIncludesBranding(t *testing.T) {
	handler := HandlerWithOptions(Options{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	body := resp.Body.String()
	if !strings.Contains(body, "LINGON") {
		t.Fatalf("expected default branding in html")
	}
}

func TestHandlerNoBannerRendersAnonymousIndex(t *testing.T) {
	handler := HandlerWithOptions(Options{NoBanner: true})

	t.Run("index", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
		}
		body := resp.Body.String()
		for _, banned := range []string{"LINGON", "Lingon", "lingon", "Sign in to your session"} {
			if strings.Contains(body, banned) {
				t.Fatalf("expected %q removed from no-banner html", banned)
			}
		}
		if strings.Contains(body, "{{") || strings.Contains(body, "}}") {
			t.Fatalf("expected rendered html template, found template markers")
		}
		if !strings.Contains(body, `name="share_token"`) {
			t.Fatalf("expected share token field in login form")
		}
	})

	t.Run("appjs", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
		}
		body := resp.Body.String()
		for _, banned := range []string{"LINGON", "Lingon", "lingon"} {
			if strings.Contains(body, banned) {
				t.Fatalf("expected %q absent from app js", banned)
			}
		}
		if !strings.Contains(body, "bifrons.fontScale") {
			t.Fatalf("expected anonymous local storage key in js")
		}
	})
}
