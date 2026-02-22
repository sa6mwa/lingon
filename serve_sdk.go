package lingon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
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
		logger = pslog.LoggerFromEnv(context.Background()).With("app", "lingon")
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
	hub := relay.NewHub(logger)
	relayServer := relay.NewHTTPServer(store, users, auth, logger, hub)
	relayServer.DataDir = cfg.Server.DataDir
	relayServer.UsersFile = cfg.Server.UsersFile
	relayServer.BasePath = base
	relayServer.WebUI.NoBanner = cfg.Server.WebUI.NoBanner
	wallInactiveLevels, err := ParseWallInactiveAfterLevels(cfg.Server.Wall.InactiveAfter)
	if err != nil {
		return fmt.Errorf("invalid wall inactivity levels: %w", err)
	}
	relayServer.ConfigureWall(cfg.Server.Wall.Timeout, wallInactiveLevels)
	relayServer.ConnectLimiter = relay.NewConnectLimiter(relay.ConnectLimitConfig{
		Disable:  cfg.Server.ConnectLimit.Disable,
		Burst:    cfg.Server.ConnectLimit.Burst,
		Count:    cfg.Server.ConnectLimit.Count,
		Window:   cfg.Server.ConnectLimit.Window,
		Headroom: 3,
	})
	if err := relay.StartUserReloadLoop(ctx, cfg.Server.UsersFile, users, logger); err != nil {
		logger.Warn("relay.users.reload.disabled", "err", err)
	}

	srvCfg := server.Config{
		ListenAddr: cfg.Server.Listen,
		DataDir:    cfg.Server.DataDir,
		BasePath:   base,
		TLSConfig:  tlsCfg,
		Logger:     logger,
		// Avoid ReadTimeout/WriteTimeout to allow long-lived WSS connections.
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	handler := server.WrapBasePath(base, relayServer.Handler())
	handler = server.AccessLog(logger, handler)
	srv := server.NewServer(srvCfg, handler)
	if srvCfg.TLSConfig == nil {
		return fmt.Errorf("tls config is required")
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("serve.http.start.init", "listen", srvCfg.ListenAddr, "base", base, "tls_mode", cfg.Server.TLS.Mode)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServeTLS("", "")
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		relayServer.Close("shutdown")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		err := <-errCh
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
