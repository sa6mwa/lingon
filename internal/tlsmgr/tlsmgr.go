package tlsmgr

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"os"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
	"pkt.systems/pslog"
)

// Mode selects how TLS is configured.
type Mode string

const (
	// ModeAuto generates or loads local TLS assets.
	ModeAuto Mode = "auto"
	// ModeBundle loads TLS assets from PEM bundle files.
	ModeBundle Mode = "bundle"
	// ModeACME uses ACME (TLS-ALPN-01) to obtain certificates.
	ModeACME Mode = "acme"
)

// Config configures TLS management behavior.
type Config struct {
	Mode        Mode
	BundleFiles []string
	Hostname    string
	Dir         string
	CacheDir    string
}

// ResolveMode chooses a TLS mode based on config inputs.
func ResolveMode(cfg Config) (Mode, error) {
	if cfg.Mode != "" {
		return cfg.Mode, nil
	}
	if len(cfg.BundleFiles) > 0 {
		return ModeBundle, nil
	}
	return ModeAuto, nil
}

// BuildServerTLSConfig builds a TLS config based on the provided settings.
func BuildServerTLSConfig(ctx context.Context, cfg Config, logger pslog.Logger) (*tls.Config, error) {
	mode, err := ResolveMode(cfg)
	if err != nil {
		return nil, err
	}

	switch mode {
	case ModeBundle:
		if len(cfg.BundleFiles) == 0 {
			return nil, fmt.Errorf("tls bundle mode requires at least one bundle file")
		}
		cert, err := LoadBundle(cfg.BundleFiles)
		if err != nil {
			return nil, err
		}
		return &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
		}, nil
	case ModeACME:
		if cfg.Hostname == "" {
			return nil, fmt.Errorf("acme mode requires --tls-hostname")
		}
		cacheDir := cfg.CacheDir
		if cacheDir == "" {
			return nil, fmt.Errorf("acme mode requires tls cache dir")
		}
		if err := os.MkdirAll(cacheDir, 0o700); err != nil {
			return nil, err
		}
		manager := autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			Cache:      autocert.DirCache(cacheDir),
			HostPolicy: autocert.HostWhitelist(cfg.Hostname),
		}
		if logger != nil {
			logger.Info("acme tls enabled", "hostname", cfg.Hostname, "cache_dir", cacheDir)
		}
		return &tls.Config{
			MinVersion:     tls.VersionTLS12,
			GetCertificate: manager.GetCertificate,
			NextProtos:     []string{acme.ALPNProto, "h2", "http/1.1"},
		}, nil
	case ModeAuto:
		if cfg.Dir == "" {
			return nil, fmt.Errorf("auto tls mode requires tls dir")
		}
		cert, err := EnsureLocalServerCert(ctx, cfg.Dir, cfg.Hostname, logger)
		if err != nil {
			return nil, err
		}
		return &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported tls mode: %s", cfg.Mode)
	}
}

func ensureLogger(logger pslog.Logger) pslog.Logger {
	if logger != nil {
		return logger
	}
	return pslog.LoggerFromEnv()
}

func wrapMissing(err error, hint string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s: %w", hint, err)
	}
	return err
}
