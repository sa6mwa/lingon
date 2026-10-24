//go:build linux

package session

import (
	"context"
	"testing"
)

func TestShutdownPTYReaderLinuxWaitsBeforeClose(t *testing.T) {
	readerDone := make(chan struct{})
	events := make(chan string, 3)

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		shutdownPTYReader(
			func() { events <- "stop" },
			func() { events <- "close" },
			readerDone,
		)
	}()

	if got := <-events; got != "stop" {
		t.Fatalf("first event = %q, want stop", got)
	}
	select {
	case got := <-events:
		t.Fatalf("close ran before reader exited: %q", got)
	default:
	}

	close(readerDone)
	if got := <-events; got != "close" {
		t.Fatalf("second event = %q, want close", got)
	}
	<-shutdownDone
}

func TestShutdownPTYReaderLinuxCancelsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	readerDone := make(chan struct{})
	close(readerDone)

	shutdownPTYReader(cancel, func() {}, readerDone)

	if err := ctx.Err(); err != context.Canceled {
		t.Fatalf("ctx.Err() = %v, want %v", err, context.Canceled)
	}
}
