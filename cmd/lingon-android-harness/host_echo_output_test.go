package main

import (
	"bufio"
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type gatedHostEchoSink struct {
	first   atomic.Bool
	entered chan struct{}
	release chan struct{}
	mu      sync.Mutex
	text    strings.Builder
}

func (w *gatedHostEchoSink) Write(p []byte) (int, error) {
	if w.first.CompareAndSwap(false, true) {
		close(w.entered)
		<-w.release
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.text.Write(p)
}

func TestHostEchoRowsRemainIntactDuringConcurrentTickAndInput(t *testing.T) {
	sink := &gatedHostEchoSink{entered: make(chan struct{}), release: make(chan struct{})}
	output := &hostEchoOutput{writer: bufio.NewWriterSize(sink, 4096)}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(sink.release) }) }
	t.Cleanup(unblock)
	firstDone, secondDone := make(chan struct{}), make(chan struct{})
	go func() { output.printf("TICK_%s %d\r\n", "host-1", 1); close(firstDone) }()
	select {
	case <-sink.entered:
	case <-ctx.Done():
		t.Fatal("first fixture row did not reach output")
	}
	secondStarted := make(chan struct{})
	go func() { close(secondStarted); output.printf("ECHO_%s %02x\r\n", "host-1", 13); close(secondDone) }()
	<-secondStarted
	unblock()
	for _, done := range []<-chan struct{}{firstDone, secondDone} {
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal("fixture writer stalled")
		}
	}
	sink.mu.Lock()
	got := sink.text.String()
	sink.mu.Unlock()
	if got != "TICK_host-1 1\r\nECHO_host-1 0d\r\n" {
		t.Fatalf("concurrent fixture rows corrupted: %q", got)
	}
}
