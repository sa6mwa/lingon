package lingon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/server"
	"pkt.systems/lingon/internal/tlsmgr"
	"pkt.systems/pslog"
)

// ServeOptions configures the relay/server run.
type ServeOptions struct {
	Config Config
	Logger pslog.Logger
}

// Serve runs the Lingon relay server.
func Serve(ctx context.Context, opts ServeOptions) error {
	cfg := opts.Config
	logger := opts.Logger
	if logger == nil {
		logger = pslog.LoggerFromEnv()
	}

	base, err := server.NormalizeBasePath(cfg.Server.BasePath)
	if err != nil {
		return err
	}

	tlsCfg, err := tlsmgr.BuildServerTLSConfig(
		ctx,
		tlsmgr.Config{
			Mode:        tlsmgr.Mode(strings.ToLower(cfg.Server.TLS.Mode)),
			BundleFiles: cfg.Server.TLS.Bundle,
			Hostname:    cfg.Server.TLS.Hostname,
			Dir:         cfg.Server.TLS.Dir,
			CacheDir:    cfg.Server.TLS.CacheDir,
		},
		logger,
	)
	if err != nil {
		return err
	}

	store, err := relay.LoadStore(cfg.Server.DataDir)
	if err != nil {
		return err
	}
	users, err := relay.LoadUserStore(cfg.Server.UsersFile)
	if err != nil {
		return err
	}

	auth := relay.NewAuthenticator(users)
	hub := relay.NewHub(logger.With("component", "relay-hub"))
	relayServer := relay.NewHTTPServer(store, users, auth, logger.With("component", "relay"), hub)
	relayServer.DataDir = cfg.Server.DataDir
	relayServer.UsersFile = cfg.Server.UsersFile
	if err := relay.StartUserReloadLoop(ctx, cfg.Server.UsersFile, users, logger.With("component", "user-watch")); err != nil {
		logger.Warn("user reload loop disabled", "err", err)
	}

	srvCfg := server.Config{
		ListenAddr: cfg.Server.Listen,
		DataDir:    cfg.Server.DataDir,
		BasePath:   base,
		TLSConfig:  tlsCfg,
		Logger:     logger.With("component", "http"),
		// Avoid ReadTimeout/WriteTimeout to allow long-lived WSS connections.
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	handler := server.WrapBasePath(base, relayServer.Handler())
	handler = server.AccessLog(logger.With("component", "access"), handler)
	srv := server.NewServer(srvCfg, handler)
	if srvCfg.TLSConfig == nil {
		return fmt.Errorf("tls config is required")
	}

	logger.Info("starting server", "listen", srvCfg.ListenAddr, "base", base, "tls_mode", cfg.Server.TLS.Mode)
	return srv.ListenAndServeTLS("", "")
}
