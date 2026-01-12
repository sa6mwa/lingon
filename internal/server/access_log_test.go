package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pkt.systems/pslog"
)

func TestAccessLogEmitsStatusAndIP(t *testing.T) {
	var buf bytes.Buffer
	logger := pslog.NewWithOptions(&buf, pslog.Options{
		Mode:             pslog.ModeStructured,
		DisableTimestamp: true,
		NoColor:          true,
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	rec := httptest.NewRecorder()

	AccessLog(logger, handler).ServeHTTP(rec, req)

	logs := buf.String()
	if !strings.Contains(logs, "\"status\":401") {
		t.Fatalf("expected status in log, got %s", logs)
	}
	if !strings.Contains(logs, "\"ip\":\"203.0.113.9\"") {
		t.Fatalf("expected ip in log, got %s", logs)
	}
}
