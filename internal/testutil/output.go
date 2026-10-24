package testutil

import (
	"bytes"
	"sync"
)

// OutputBuffer is a writer whose immutable snapshots are safe to inspect while
// a test-owned asynchronous producer is writing.
type OutputBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends one complete write under the observer lock.
func (b *OutputBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(data)
}

// String returns an immutable snapshot of all observed output.
func (b *OutputBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Reset starts a new capture without racing the asynchronous writer.
func (b *OutputBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}
