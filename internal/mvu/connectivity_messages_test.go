package mvu

import (
	"testing"
	"time"
)

func TestConnectedToMessage(t *testing.T) {
	if got := ConnectedToMessage("https://localhost:1234/v1"); got != "connected to https://localhost:1234/v1" {
		t.Fatalf("ConnectedToMessage(endpoint) = %q", got)
	}
	if got := ConnectedToMessage(""); got != "connected" {
		t.Fatalf("ConnectedToMessage(empty) = %q", got)
	}
}

func TestConnectionLostMessage(t *testing.T) {
	if got := ConnectionLostMessage("https://localhost:1234/v1"); got != "connection lost to https://localhost:1234/v1, reconnecting" {
		t.Fatalf("ConnectionLostMessage(endpoint) = %q", got)
	}
	if got := ConnectionLostMessage(""); got != "connection lost, reconnecting" {
		t.Fatalf("ConnectionLostMessage(empty) = %q", got)
	}
}

func TestConnectionLostBackoffMessage(t *testing.T) {
	if got := ConnectionLostBackoffMessage("https://localhost:1234/v1", 2500*time.Millisecond); got != "connection lost to https://localhost:1234/v1, reconnecting in 3s" {
		t.Fatalf("ConnectionLostBackoffMessage(endpoint,+2.5s) = %q", got)
	}
	if got := ConnectionLostBackoffMessage("", -time.Second); got != "connection lost, reconnecting in 0s" {
		t.Fatalf("ConnectionLostBackoffMessage(empty,-1s) = %q", got)
	}
}
