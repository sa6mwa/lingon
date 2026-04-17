package terminal

import (
	"io"
	"sync"
)

// NewLockedWriter returns a writer that serializes writes to w.
//
// When mu is non-nil, the returned writer shares that lock with other callers.
// This is required when multiple renderers write escape-sequence frames to the
// same terminal device through different call paths.
func NewLockedWriter(w io.Writer, mu *sync.Mutex) io.Writer {
	if w == nil {
		return nil
	}
	if mu == nil {
		mu = &sync.Mutex{}
	}
	return &lockedWriter{w: w, mu: mu}
}

type lockedWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}
