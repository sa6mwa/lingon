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
