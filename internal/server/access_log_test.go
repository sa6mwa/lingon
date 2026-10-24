package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"pkt.systems/pslog"
)

func TestAccessLogEmitsStatusAndIP(t *testing.T) {
	logger := &recordingLogger{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	rec := httptest.NewRecorder()

	AccessLog(logger, handler).ServeHTTP(rec, req)

	entry := logger.singleEntry(t)
	if entry.level != pslog.WarnLevel {
		t.Fatalf("log level = %v, want %v", entry.level, pslog.WarnLevel)
	}
	if got := entry.field("status"); got != http.StatusUnauthorized {
		t.Fatalf("status = %v, want %d", got, http.StatusUnauthorized)
	}
	if got := entry.field("ip"); got != "203.0.113.9" {
		t.Fatalf("ip = %v, want 203.0.113.9", got)
	}
	if entry.message != "server.http.request.done" {
		t.Fatalf("message = %q, want server.http.request.done", entry.message)
	}
}

func TestAccessLogUsesResponseSeverity(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		wantLevel pslog.Level
	}{
		{name: "successful request", status: http.StatusOK, wantLevel: pslog.TraceLevel},
		{name: "client error", status: http.StatusBadRequest, wantLevel: pslog.WarnLevel},
		{name: "server error", status: http.StatusInternalServerError, wantLevel: pslog.ErrorLevel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &recordingLogger{}
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			})

			AccessLog(logger, handler).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/resource", nil))

			if got := logger.singleEntry(t).level; got != tt.wantLevel {
				t.Fatalf("log level = %v, want %v", got, tt.wantLevel)
			}
		})
	}
}

type logEntry struct {
	level   pslog.Level
	message string
	fields  []any
}

func (e logEntry) field(want string) any {
	for i := 0; i+1 < len(e.fields); i += 2 {
		if e.fields[i] == want {
			return e.fields[i+1]
		}
	}
	return nil
}

type recordingLogger struct {
	pslog.Logger
	entries []logEntry
}

func (l *recordingLogger) Trace(message string, fields ...any) {
	l.record(pslog.TraceLevel, message, fields)
}

func (l *recordingLogger) Debug(message string, fields ...any) {
	l.record(pslog.DebugLevel, message, fields)
}

func (l *recordingLogger) Info(message string, fields ...any) {
	l.record(pslog.InfoLevel, message, fields)
}

func (l *recordingLogger) Warn(message string, fields ...any) {
	l.record(pslog.WarnLevel, message, fields)
}

func (l *recordingLogger) Error(message string, fields ...any) {
	l.record(pslog.ErrorLevel, message, fields)
}

func (l *recordingLogger) record(level pslog.Level, message string, fields []any) {
	l.entries = append(l.entries, logEntry{
		level:   level,
		message: message,
		fields:  append([]any(nil), fields...),
	})
}

func (l *recordingLogger) singleEntry(t *testing.T) logEntry {
	t.Helper()
	if len(l.entries) != 1 {
		t.Fatalf("log entries = %d, want 1", len(l.entries))
	}
	return l.entries[0]
}
