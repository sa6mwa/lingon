package server

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"time"

	"pkt.systems/pslog"
)

// AccessLog wraps an HTTP handler with request logging.
func AccessLog(logger pslog.Logger, handler http.Handler) http.Handler {
	if handler == nil {
		handler = http.DefaultServeMux
	}
	if logger == nil {
		logger = pslog.LoggerFromEnv()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		handler.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		duration := time.Since(start)
		ip := RealIP(r)
		fields := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration", duration.String(),
			"ip", ip,
		}
		switch {
		case rec.status >= 500:
			logger.Error("http request", fields...)
		case rec.status >= 400:
			logger.Warn("http request", fields...)
		default:
			logger.Info("http request", fields...)
		}
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

func (s *statusRecorder) Flush() {
	if flusher, ok := s.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := s.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijacker not supported")
	}
	return hijacker.Hijack()
}

func (s *statusRecorder) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := s.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}
