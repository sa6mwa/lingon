package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestShareAuthFaultsAreFiniteAndIsolated(t *testing.T) {
	faults := &shareAuthFaults{}
	handler := faults.wrap("/v1", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodPost, "/v1/__harness/share-auth-failures?count=2", 200},
		{http.MethodGet, "/v1/auth/share", 204},
		{http.MethodPost, "/v1/auth/login", 204},
		{http.MethodPost, "/v1/auth/share", 503},
		{http.MethodPost, "/v1/auth/share", 503},
		{http.MethodPost, "/v1/auth/share", 204},
		{http.MethodPost, "/v1/__harness/share-auth-failures?count=11", 400},
		{http.MethodDelete, "/v1/__harness/share-auth-failures", 405},
	} {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(tc.method, tc.path, nil))
		if rr.Code != tc.want {
			t.Fatalf("%s %s: %d, want %d", tc.method, tc.path, rr.Code, tc.want)
		}
	}
	if faults.remaining.Load() != 0 {
		t.Fatal("faults remained after recovery")
	}
}
