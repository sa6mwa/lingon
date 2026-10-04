package sessionmanage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestManagementCallDoesNotRetryAnUncertainResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/sessions/manage" || r.Header.Get("Authorization") != "Bearer owner" {
			t.Errorf("unexpected management request: %s %s", r.Method, r.URL)
		}
		_, _ = w.Write([]byte(`{"session_id":`))
	}))
	defer server.Close()
	_, err := Call(context.Background(), server.Client(), server.URL, "owner", Request{RequestID: "once", SessionID: "host", Kind: "create"})
	if err == nil || !strings.Contains(err.Error(), "outcome unknown") || calls.Load() != 1 {
		t.Fatalf("calls=%d error=%v", calls.Load(), err)
	}
}
