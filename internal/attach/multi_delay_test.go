package attach

import (
	"testing"
	"time"

	"pkt.systems/lingon/internal/backoff"
)

func TestNormalizeReconnectDelay(t *testing.T) {
	if got := normalizeReconnectDelay(3*time.Second, time.Second); got != 3*time.Second {
		t.Fatalf("normalizeReconnectDelay(3s, 1s) = %v, want 3s", got)
	}
	if got := normalizeReconnectDelay(0, 500*time.Millisecond); got != 500*time.Millisecond {
		t.Fatalf("normalizeReconnectDelay(0, 500ms) = %v, want 500ms", got)
	}
	if got := normalizeReconnectDelay(-time.Second, 500*time.Millisecond); got != 500*time.Millisecond {
		t.Fatalf("normalizeReconnectDelay(-1s, 500ms) = %v, want 500ms", got)
	}
}

func TestNormalizeReconnectDelayFallsBackToDefaultBase(t *testing.T) {
	if got := normalizeReconnectDelay(0, 0); got != backoff.DefaultPolicy.Base {
		t.Fatalf("normalizeReconnectDelay(0, 0) = %v, want %v", got, backoff.DefaultPolicy.Base)
	}
}
