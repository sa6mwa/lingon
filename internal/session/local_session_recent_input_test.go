package session

import (
	"context"
	"strings"
	"testing"
)

func TestLocalSessionRecentInputRing(t *testing.T) {
	sess := newLocalSession(context.Background(), localSessionOptions{})

	sess.recordRecentInput([]byte("hello"))
	sess.recordRecentInput([]byte("-world"))
	got := string(sess.recentInputSnapshot())
	if got != "hello-world" {
		t.Fatalf("expected recent input %q, got %q", "hello-world", got)
	}

	payload := strings.Repeat("a", recentInputLimit+10)
	sess.recordRecentInput([]byte(payload))
	got = string(sess.recentInputSnapshot())
	if len(got) != recentInputLimit {
		t.Fatalf("expected recent input length %d, got %d", recentInputLimit, len(got))
	}
	if got != payload[len(payload)-recentInputLimit:] {
		t.Fatalf("expected recent input to contain tail of payload")
	}
}

func TestAnalyzeRecentInputSignals(t *testing.T) {
	data := []byte("\x1b[6n\x1b[?1049h\x1b[2J\x1b[H\x1b[1;1H\x1b[3;4H\x1b[r\x1bc")
	signals := analyzeRecentInput(data)
	if !signals.hasDSR {
		t.Fatalf("expected DSR detection")
	}
	if !signals.hasAltEnter {
		t.Fatalf("expected alt-screen enter detection")
	}
	if !signals.hasClear || !signals.hasED || !signals.hasED2 {
		t.Fatalf("expected clear/ED detection")
	}
	if !signals.hasCUP || !signals.hasCUPHome {
		t.Fatalf("expected CUP detection")
	}
	if !signals.hasScrollRegion {
		t.Fatalf("expected scroll region detection")
	}
	if !signals.hasRIS {
		t.Fatalf("expected RIS detection")
	}
}
