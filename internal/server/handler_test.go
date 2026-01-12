package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWrapBasePath(t *testing.T) {
	h := http.NewServeMux()
	h.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := WrapBasePath("/base", h)
	request := httptest.NewRequest(http.MethodGet, "/base/health", nil)
	resp := httptest.NewRecorder()
	wrapped.ServeHTTP(resp, request)
	if resp.Result().StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Result().StatusCode, http.StatusOK)
	}
}
