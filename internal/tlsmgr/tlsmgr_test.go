package tlsmgr

import "testing"

func TestBuildServerTLSConfigBundleRequiresFiles(t *testing.T) {
	_, err := BuildServerTLSConfig(t.Context(), Config{Mode: ModeBundle}, nil)
	if err == nil {
		t.Fatalf("expected error for empty bundle")
	}
}

func TestBuildServerTLSConfigACMERequiresHostname(t *testing.T) {
	_, err := BuildServerTLSConfig(t.Context(), Config{Mode: ModeACME, CacheDir: t.TempDir()}, nil)
	if err == nil {
		t.Fatalf("expected error for missing hostname")
	}
}

func TestBuildServerTLSConfigAutoGenerates(t *testing.T) {
	dir := t.TempDir()
	cfg, err := BuildServerTLSConfig(t.Context(), Config{Mode: ModeAuto, Dir: dir}, nil)
	if err != nil {
		t.Fatalf("BuildServerTLSConfig: %v", err)
	}
	if cfg == nil || len(cfg.Certificates) == 0 {
		t.Fatalf("expected TLS config with certificate")
	}
}
