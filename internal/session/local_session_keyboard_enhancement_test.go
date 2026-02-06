package session

import (
	"io"
	"os"
	"testing"

	"pkt.systems/lingon/internal/terminal"
)

func TestRespondToTerminalQueriesKeyboardEnhancement(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer func() {
		_ = reader.Close()
		_ = writer.Close()
	}()

	session := &localSession{}
	session.ptyMu.Lock()
	session.pty = writer
	session.ptyMu.Unlock()

	session.respondToTerminalQueries([]byte("\x1b[?u"), terminal.Snapshot{})

	got := make([]byte, 5)
	if _, err := io.ReadFull(reader, got); err != nil {
		t.Fatalf("read response: %v", err)
	}
	if string(got) != "\x1b[?0u" {
		t.Fatalf("expected response ESC[?0u, got %q", string(got))
	}
}
