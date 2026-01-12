package tlsmgr

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBundleFromSingleFile(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateAll(t.Context(), dir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}

	certPath := filepath.Join(dir, serverCertFilename)
	keyPath := filepath.Join(dir, serverKeyFilename)

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}

	bundlePath := filepath.Join(dir, "bundle.pem")
	if err := os.WriteFile(bundlePath, append(certPEM, keyPEM...), 0o600); err != nil {
		t.Fatalf("write bundle: %v", err)
	}

	cert, err := LoadBundle([]string{bundlePath})
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatalf("expected certificate data")
	}
}

func TestLoadBundleFromMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateAll(t.Context(), dir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}

	certPath := filepath.Join(dir, serverCertFilename)
	keyPath := filepath.Join(dir, serverKeyFilename)

	cert, err := LoadBundle([]string{certPath, keyPath})
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatalf("expected certificate data")
	}
}

func TestLoadBundleMissingKey(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateAll(t.Context(), dir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}

	certPath := filepath.Join(dir, serverCertFilename)
	if _, err := LoadBundle([]string{certPath}); err == nil {
		t.Fatalf("expected error for missing key")
	}
}
