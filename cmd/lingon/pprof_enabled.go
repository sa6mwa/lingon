//go:build pprof

package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"
	"sync"
	"syscall"
)

const (
	defaultPprofAddr = "127.0.0.1:12606"
	pprofAddrEnvKey  = "LINGON_PPROF_ADDR"
)

var (
	pprofStartOnce sync.Once
	pprofStartErr  error
)

func startPprofServer() error {
	pprofStartOnce.Do(func() {
		addr := defaultPprofAddr
		if raw := strings.TrimSpace(os.Getenv(pprofAddrEnvKey)); raw != "" {
			addr = raw
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			if errors.Is(err, syscall.EADDRINUSE) {
				return
			}
			pprofStartErr = fmt.Errorf("start pprof listener on %s: %w", addr, err)
			return
		}
		srv := &http.Server{Handler: pprofMux()}
		go func() {
			_ = srv.Serve(ln)
		}()
	})
	return pprofStartErr
}

func pprofMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}
