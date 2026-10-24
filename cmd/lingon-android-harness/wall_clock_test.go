package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
)

func TestHarnessWallClockHTTPControl(t *testing.T) {
	for _, name := range []string{"LINGON_CONFIG_DIR", "LINGON_TERMINAL_DISABLE_DESKTOP_NOTIFICATIONS", "LINGON_DEBUG_INPUT", "LINGON_HOST_PUBLISHER_PING_INTERVAL", "LINGON_HOST_PUBLISHER_PING_TIMEOUT", "LINGON_HOST_ECHO_LOG"} {
		t.Setenv(name, "")
	}
	clk := clock.NewMock()
	h, err := startHarness(context.Background(), harnessOptions{
		wallClock: clk, basePath: "/v1", username: "clock-test", password: "test-only", cols: 80, rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.stop)
	for _, tc := range []struct {
		name, method, body string
		status             int
		advance            time.Duration
	}{
		{"wrong method", http.MethodGet, "", http.StatusMethodNotAllowed, 0},
		{"invalid json", http.MethodPost, "{", http.StatusBadRequest, 0},
		{"negative", http.MethodPost, `{"milliseconds":-1}`, http.StatusBadRequest, 0},
		{"too large", http.MethodPost, `{"milliseconds":3600001}`, http.StatusBadRequest, 0},
		{"advance", http.MethodPost, `{"milliseconds":999}`, http.StatusOK, 999 * time.Millisecond},
		{"zero", http.MethodPost, `{"milliseconds":0}`, http.StatusOK, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := clk.Now()
			req, err := http.NewRequest(tc.method, h.config.Endpoint+"/__harness/advance-wall-clock", strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			resp, err := h.server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
			if resp.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.status)
			}
			if tc.method != http.MethodPost && resp.Header.Get("Allow") != http.MethodPost {
				t.Fatal("missing allowed method")
			}
			if got := clk.Now().Sub(before); got != tc.advance {
				t.Fatalf("clock advanced %s, want %s", got, tc.advance)
			}
		})
	}
}

func TestHarnessWallClockAdvancementIsExplicitAndBounded(t *testing.T) {
	clk := clock.NewMock()
	h := &harness{wallClock: clk}
	fired := make(chan struct{}, 1)
	clk.AfterFunc(250*time.Millisecond, func() { fired <- struct{}{} })
	if err := h.advanceWallClock(249); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fired:
		t.Fatal("timer fired before controlled deadline")
	default:
	}
	if err := h.advanceWallClock(1); err != nil {
		t.Fatal(err)
	}
	<-fired
	before := clk.Now()
	for _, value := range []int64{-1, 3600001} {
		if err := h.advanceWallClock(value); err == nil {
			t.Fatal("invalid clock advance accepted")
		}
	}
	if !clk.Now().Equal(before) {
		t.Fatal("invalid request advanced the clock")
	}
	if err := (&harness{}).advanceWallClock(250); err == nil {
		t.Fatal("missing controlled clock silently accepted")
	}
	if err := (&harness{wallClock: clock.New()}).advanceWallClock(250); err == nil {
		t.Fatal("real clock silently accepted")
	}
}
