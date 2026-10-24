//go:build !linux

package session

import "testing"

func TestShutdownPTYReaderNonLinuxClosesBeforeWaiting(t *testing.T) {
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
	if got := <-events; got != "close" {
		t.Fatalf("second event = %q, want close", got)
	}
	select {
	case <-shutdownDone:
		t.Fatalf("shutdown returned before reader exited")
	default:
	}

	close(readerDone)
	<-shutdownDone
}
