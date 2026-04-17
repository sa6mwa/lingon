package terminal

import (
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type interleavingWriter struct {
	mu     sync.Mutex
	buf    strings.Builder
	active atomic.Int32
}

func (w *interleavingWriter) Write(p []byte) (int, error) {
	w.active.Add(1)
	defer w.active.Add(-1)
	for i := 0; i < 2048 && w.active.Load() < 2; i++ {
		runtime.Gosched()
	}
	for _, b := range p {
		w.mu.Lock()
		_ = w.buf.WriteByte(b)
		w.mu.Unlock()
		runtime.Gosched()
	}
	return len(p), nil
}

func (w *interleavingWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func assertWholeChunkOrder(t *testing.T, got string, chunks ...string) {
	t.Helper()
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if got == chunks[0]+chunks[1] || got == chunks[1]+chunks[0] {
		return
	}
	t.Fatalf("output interleaved or corrupted:\n%q", got)
}

func TestNewLockedWriterSerializesConcurrentControlStreamWrites(t *testing.T) {
	payloadA := "\x1bPtmux;\x1b[31mattach-wall-connect\x1b\\\x1b[0m"
	payloadB := "\x1b]10;rgb:dddd/0000/0000\a[server exited unexpectedly]"
	writer := &interleavingWriter{}
	locked := NewLockedWriter(writer, nil)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.WriteString(locked, payloadA)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.WriteString(locked, payloadB)
	}()
	wg.Wait()

	assertWholeChunkOrder(t, writer.String(), payloadA, payloadB)
}

func TestNewLockedWriterSharesExternalTerminalMutex(t *testing.T) {
	payloadA := "\x1b[?1049h\x1b[H\x1b[2Jconnected to https://127.0.0.1:43245/v1"
	payloadB := "\x1b]11;rgb:0000/0000/0000\a[server exited unexpectedly]"
	writer := &interleavingWriter{}
	var stdoutMu sync.Mutex
	locked := NewLockedWriter(writer, &stdoutMu)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.WriteString(locked, payloadA)
	}()
	go func() {
		defer wg.Done()
		stdoutMu.Lock()
		defer stdoutMu.Unlock()
		_, _ = io.WriteString(writer, payloadB)
	}()
	wg.Wait()

	assertWholeChunkOrder(t, writer.String(), payloadA, payloadB)
}
