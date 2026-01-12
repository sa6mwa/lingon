package server

import (
	"context"
	"crypto/tls"
	"net/http"
	"time"

	"pkt.systems/pslog"
)

// Config configures the HTTP server.
type Config struct {
	ListenAddr string
	DataDir    string
	BasePath   string
	TLSConfig  *tls.Config
	Logger     pslog.Logger

	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	MaxHeaderBytes    int
}

// Server abstracts the HTTP server for Lingon.
type Server interface {
	ListenAndServe() error
	ListenAndServeTLS(certFile, keyFile string) error
	Shutdown(ctx context.Context) error
}

type stdServer struct {
	srv *http.Server
}

// NewServer constructs a Server using the provided handler.
func NewServer(cfg Config, handler http.Handler) Server {
	logger := cfg.Logger
	if logger == nil {
		logger = pslog.LoggerFromEnv()
	}
	errorLog := pslog.LogLogger(logger)
	return &stdServer{
		srv: &http.Server{
			Addr:              cfg.ListenAddr,
			Handler:           handler,
			TLSConfig:         cfg.TLSConfig,
			ErrorLog:          errorLog,
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			ReadTimeout:       cfg.ReadTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
			MaxHeaderBytes:    cfg.MaxHeaderBytes,
		},
	}
}

func (s *stdServer) ListenAndServe() error {
	return s.srv.ListenAndServe()
}

func (s *stdServer) ListenAndServeTLS(certFile, keyFile string) error {
	return s.srv.ListenAndServeTLS(certFile, keyFile)
}

func (s *stdServer) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
