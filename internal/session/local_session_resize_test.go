package session

import (
	"context"
	"testing"
)

func TestLocalSessionResizeBeforeRunDoesNotPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sess := newLocalSession(ctx, localSessionOptions{
		ID:   "local-resize",
		Name: "local-resize",
		Cols: 80,
		Rows: 24,
	})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Resize panicked before Run: %v", r)
		}
	}()

	if _, err := sess.Resize(99, 29); err == nil {
		t.Fatalf("expected error when resizing before emulator init")
	}
}

func TestLocalSessionResizeRedrawSuppressionDoesNotHidePendingInputOutput(t *testing.T) {
	sess := &localSession{}

	sess.markPTYInputPending()
	sess.armIgnoreNextPTYOutput()

	if sess.shouldIgnoreNextPTYOutput([]byte("\x1b[?2004l")) {
		t.Fatalf("input-adjacent redraw output must not be suppressed")
	}
	sess.armIgnoreNextPTYOutput()
	if sess.shouldIgnoreNextPTYOutput([]byte("\x1b[?2004l\recho TOKEN\r\nTOKEN\r\n")) {
		t.Fatalf("input output must be published even when resize redraw suppression was armed")
	}
	if sess.shouldIgnoreNextPTYOutput([]byte("\x1b[?2004l")) {
		t.Fatalf("input output should clear resize redraw suppression")
	}
}

func TestLocalSessionResizeRedrawSuppressionStillSuppressesPureRedraw(t *testing.T) {
	sess := &localSession{}

	sess.armIgnoreNextPTYOutput()

	if !sess.shouldIgnoreNextPTYOutput([]byte("\x1b[?2004l\x1b[?2004h")) {
		t.Fatalf("pure resize redraw escape output should still be suppressed")
	}
	if sess.shouldIgnoreNextPTYOutput([]byte("\r\n")) {
		t.Fatalf("line output should clear resize redraw suppression")
	}
}
