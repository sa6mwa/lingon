package attach

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"pkt.systems/lingon/internal/testutil"
	"pkt.systems/lingon/internal/tlsmgr"
)

func TestClientTLSConfigUsesProvidedDir(t *testing.T) {
	tlsDir := filepath.Join(testutil.TempDir(t), "tls")
	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	tlsCfg, err := clientTLSConfig(tlsDir, false)
	if err != nil {
		t.Fatalf("clientTLSConfig: %v", err)
	}
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
	}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	resp.Body.Close()
}

func TestClientTLSConfigInsecure(t *testing.T) {
	cfg, err := clientTLSConfig("", true)
	if err != nil {
		t.Fatalf("clientTLSConfig: %v", err)
	}
	if !cfg.InsecureSkipVerify {
		t.Fatalf("expected InsecureSkipVerify=true")
	}
}
