package tlsmgr

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"

	"pkt.systems/lingon/internal/testutil"
)

func TestGenerateAllCreatesTLSAssets(t *testing.T) {
	dir := testutil.TempDir(t)
	if err := GenerateAll(t.Context(), dir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}

	paths := []string{
		filepath.Join(dir, caCertFilename),
		filepath.Join(dir, caKeyFilename),
		filepath.Join(dir, serverCertFilename),
		filepath.Join(dir, serverKeyFilename),
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}

	cert, err := LoadLocalServerCert(dir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatalf("expected certificate data")
	}

	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	wantDNS := map[string]bool{"localhost": true}
	wantIP := map[string]bool{
		"127.0.0.1": true,
		"::1":       true,
		"10.0.2.2":  true,
	}
	for _, dns := range parsed.DNSNames {
		delete(wantDNS, dns)
	}
	for _, ip := range parsed.IPAddresses {
		delete(wantIP, ip.String())
	}
	if len(wantDNS) != 0 || len(wantIP) != 0 {
		t.Fatalf("missing SANs: dns=%v ips=%v", wantDNS, wantIP)
	}
}

func TestGenerateServerCertRequiresCA(t *testing.T) {
	dir := testutil.TempDir(t)
	if err := GenerateServerCert(t.Context(), dir, "", nil); err == nil {
		t.Fatalf("expected error when CA is missing")
	}
}

func TestGenerateCAThenServerCert(t *testing.T) {
	dir := testutil.TempDir(t)
	if err := GenerateCA(t.Context(), dir, nil); err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	if err := GenerateServerCert(t.Context(), dir, "", nil); err != nil {
		t.Fatalf("GenerateServerCert: %v", err)
	}
}

func TestGenerateAllFailsIfExists(t *testing.T) {
	dir := testutil.TempDir(t)
	if err := GenerateAll(t.Context(), dir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	if err := GenerateAll(t.Context(), dir, "", nil); err == nil {
		t.Fatalf("expected error when TLS assets already exist")
	}
}
