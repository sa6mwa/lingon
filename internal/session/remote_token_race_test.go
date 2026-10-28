package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type requestContextBarrier struct {
	context.Context
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (c *requestContextBarrier) Deadline() (time.Time, bool) {
	c.once.Do(func() {
		close(c.entered)
		select {
		case <-c.release:
		case <-c.Done():
		}
	})
	return c.Context.Deadline()
}

func TestRemoteSessionsAuthorizationDuringTokenRotation(t *testing.T) {
	headers := make(chan string, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	manager := newRemoteManager(remoteOptions{Endpoint: server.URL, Token: "initial", Insecure: true})
	defer manager.closeHTTPClient()
	parent, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ctx := &requestContextBarrier{Context: parent, entered: make(chan struct{}), release: make(chan struct{})}
	fetched := make(chan error, 1)
	go func() {
		_, err := manager.fetchSessions(ctx, server.URL)
		fetched <- err
	}()
	// WithTimeout consults Deadline after sessionsHTTPClient has released mu.
	// Release the reader and writer from one barrier: neither access can gain
	// an accidental happens-before edge from setup or an elapsed-time wait.
	select {
	case <-ctx.entered:
	case <-parent.Done():
		t.Fatal(parent.Err())
	}
	rotated := make(chan struct{})
	go func() {
		<-ctx.release
		manager.setToken("rotated")
		close(rotated)
	}()
	close(ctx.release)
	if err := <-fetched; err != nil {
		t.Fatal(err)
	}
	<-rotated
	if header := <-headers; header != "Bearer initial" && header != "Bearer rotated" {
		t.Fatalf("invalid concurrent authorization: %q", header)
	}
	if _, err := manager.fetchSessions(parent, server.URL); err != nil {
		t.Fatal(err)
	}
	if header := <-headers; header != "Bearer rotated" {
		t.Fatalf("rotation was not used by the next request: %q", header)
	}
}
