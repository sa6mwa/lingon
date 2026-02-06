package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRealIPPrefersForwarded(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Forwarded", "for=192.0.2.60;proto=http;by=203.0.113.43")
	req.RemoteAddr = "10.0.0.1:1234"
	if ip := RealIP(req); ip != "192.0.2.60" {
		t.Fatalf("expected forwarded ip, got %s", ip)
	}
}

func TestRealIPPrefersXForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "198.51.100.10, 198.51.100.11")
	req.RemoteAddr = "10.0.0.1:1234"
	if ip := RealIP(req); ip != "198.51.100.10" {
		t.Fatalf("expected x-forwarded-for ip, got %s", ip)
	}
}

func TestRealIPFallsBackToRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	if ip := RealIP(req); ip != "10.0.0.1" {
		t.Fatalf("expected remote addr ip, got %s", ip)
	}
}
