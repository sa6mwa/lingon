package config

import "testing"

func TestApplyOverrides(t *testing.T) {
	cfg := BootstrapConfig()

	overrides := map[string]any{
		"server.base":            "",
		"terminal.respawn":       true,
		"server.tls.dir":         "/app/certs",
		"server.tls.cache_dir":   "tls/cache",
		"server.webui.no_banner": true,
	}

	got, err := ApplyOverrides(cfg, overrides)
	if err != nil {
		t.Fatalf("ApplyOverrides error = %v", err)
	}

	if got.Server.BasePath != "" {
		t.Fatalf("Server.BasePath = %q, want empty", got.Server.BasePath)
	}
	if got.Terminal.Respawn != true {
		t.Fatalf("Terminal.Respawn = %v, want true", got.Terminal.Respawn)
	}
	if got.Server.TLS.Dir != "/app/certs" {
		t.Fatalf("Server.TLS.Dir = %q, want /app/certs", got.Server.TLS.Dir)
	}
	if got.Server.TLS.CacheDir != "tls/cache" {
		t.Fatalf("Server.TLS.CacheDir = %q, want tls/cache", got.Server.TLS.CacheDir)
	}
	if got.Server.WebUI.NoBanner != true {
		t.Fatalf("Server.WebUI.NoBanner = %v, want true", got.Server.WebUI.NoBanner)
	}
}
