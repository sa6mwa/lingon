package host

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"

	"pkt.systems/lingon/internal/tlsmgr"
)

func TestHostDialUsesLocalCA(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	tlsDir := filepath.Join(home, ".lingon", "tls")

	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
		if err != nil {
			return
		}
		_ = conn.Close(websocket.StatusNormalClosure, "ok")
	})

	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	wsBase, err := normalizeEndpoint(server.URL)
	if err != nil {
		t.Fatalf("normalizeEndpoint: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tlsCfg, err := clientTLSConfig()
	if err != nil {
		t.Fatalf("clientTLSConfig: %v", err)
	}
	httpClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
	}
	conn, _, err := websocket.Dial(ctx, wsBase+"/ws/host", &websocket.DialOptions{HTTPClient: httpClient})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "done")
}
