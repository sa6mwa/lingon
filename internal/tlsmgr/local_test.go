package tlsmgr

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateAllCreatesTLSAssets(t *testing.T) {
	dir := t.TempDir()
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
}

func TestGenerateServerCertRequiresCA(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateServerCert(t.Context(), dir, "", nil); err == nil {
		t.Fatalf("expected error when CA is missing")
	}
}

func TestGenerateCAThenServerCert(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateCA(t.Context(), dir, nil); err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	if err := GenerateServerCert(t.Context(), dir, "", nil); err != nil {
		t.Fatalf("GenerateServerCert: %v", err)
	}
}

func TestGenerateAllFailsIfExists(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateAll(t.Context(), dir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	if err := GenerateAll(t.Context(), dir, "", nil); err == nil {
		t.Fatalf("expected error when TLS assets already exist")
	}
}
