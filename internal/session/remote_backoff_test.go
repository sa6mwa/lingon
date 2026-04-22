package session

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/netgate"
	"pkt.systems/lingon/internal/server"
	"pkt.systems/lingon/internal/testutil"
	"pkt.systems/lingon/internal/tlsmgr"
)

func waitUntilNoErr(t *testing.T, clk clock.Clock, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := clk.Now().Add(timeout)
	for clk.Now().Before(deadline) {
		if cond() {
			return
		}
		advanceClock(clk, 10*time.Millisecond)
	}
	t.Fatalf("timeout waiting for condition")
}

func TestRemoteManagerHonorsBackoffGate(t *testing.T) {
	root := testutil.SetXDGConfigEnv(t)
	configDir := filepath.Join(root, "lingon")
	tlsDir := filepath.Join(configDir, "tls")
	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}

	var sessionsCount int64
	mux := http.NewServeMux()
	mux.HandleFunc("/sessions", func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&sessionsCount, 1)
		_, _ = w.Write([]byte("[]"))
	})

	handler := server.WrapBasePath("/v1", mux)
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	endpoint := srv.URL + "/v1"
	clk := clock.NewMock()
	gate := netgate.New(clk)
	gate.BlockFor(500 * time.Millisecond)

	rm := newRemoteManager(remoteOptions{
		Endpoint:        endpoint,
		Token:           "token",
		LocalID:         "local",
		LocalName:       "local",
		InactiveTTL:     10 * time.Millisecond,
		RefreshInterval: 50 * time.Millisecond,
		Gate:            gate,
		Clock:           clk,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rm.Start(ctx)

	advanceClock(clk, 200*time.Millisecond)
	if atomic.LoadInt64(&sessionsCount) != 0 {
		t.Fatalf("expected no requests while gated, got sessions=%d", sessionsCount)
	}

	gate.Allow()
	waitUntilNoErr(t, clk, 2*time.Second, func() bool {
		return atomic.LoadInt64(&sessionsCount) > 0
	})
}

func TestNewRemoteManagerInitializesCompositorWhenMissing(t *testing.T) {
	rm := newRemoteManager(remoteOptions{
		Endpoint:  "https://relay.example/v1",
		Token:     "token",
		LocalID:   "local",
		LocalName: "local",
	})
	if rm.compositor == nil {
		t.Fatalf("expected remote manager compositor to be initialized")
	}
}
