package session

import (
	"os"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/trace"
)

func TestContainsEnter(t *testing.T) {
	if !containsEnter([]byte("hi\r")) {
		t.Fatalf("expected CR to be detected")
	}
	if !containsEnter([]byte("hi\n")) {
		t.Fatalf("expected LF to be detected")
	}
	if containsEnter([]byte("hi")) {
		t.Fatalf("did not expect enter in plain text")
	}
}

func TestNoteLocalOutputClearsPending(t *testing.T) {
	tmp, err := os.CreateTemp("", "lingon-trace-*.jsonl")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	_ = tmp.Close()
	defer func() {
		_ = os.Remove(tmp.Name())
	}()
	tw, err := trace.New(tmp.Name())
	if err != nil {
		t.Fatalf("trace writer: %v", err)
	}
	defer func() {
		_ = tw.Close()
	}()
	mock := clock.NewMock()
	r := &Runner{clock: mock, trace: tw}

	r.noteLocalEnterInput("s1", []byte("codex\r"))
	if !r.inputTracePending["s1"] {
		t.Fatalf("expected pending output after enter")
	}
	mock.Add(250 * time.Millisecond)
	r.noteLocalOutput("s1", []byte("ok"))
	if r.inputTracePending["s1"] {
		t.Fatalf("expected pending cleared after output")
	}
}
