package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRemoteManagerSessionsHTTPClientReused(t *testing.T) {
	rm := newRemoteManager(remoteOptions{
		Endpoint: "https://example.invalid",
		Token:    "token",
		Insecure: true,
	})

	clientA, err := rm.sessionsHTTPClient()
	if err != nil {
		t.Fatalf("first sessionsHTTPClient: %v", err)
	}
	clientB, err := rm.sessionsHTTPClient()
	if err != nil {
		t.Fatalf("second sessionsHTTPClient: %v", err)
	}
	if clientA != clientB {
		t.Fatalf("expected sessions HTTP client reuse")
	}

	rm.closeHTTPClient()

	clientC, err := rm.sessionsHTTPClient()
	if err != nil {
		t.Fatalf("third sessionsHTTPClient: %v", err)
	}
	if clientC == clientA {
		t.Fatalf("expected new sessions HTTP client after close")
	}
}

func TestRemoteManagerFetchSessionsHonorsTimeout(t *testing.T) {
	old := remoteSessionsRequestTimeout
	remoteSessionsRequestTimeout = 120 * time.Millisecond
	t.Cleanup(func() {
		remoteSessionsRequestTimeout = old
	})

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sessions" {
			time.Sleep(3 * remoteSessionsRequestTimeout)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)

	rm := newRemoteManager(remoteOptions{
		Endpoint: server.URL,
		Token:    "token",
		Insecure: true,
	})

	start := time.Now()
	_, err := rm.fetchSessions(context.Background(), server.URL)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if elapsed > 5*remoteSessionsRequestTimeout {
		t.Fatalf("fetch sessions took too long: %v (timeout %v)", elapsed, remoteSessionsRequestTimeout)
	}
}
