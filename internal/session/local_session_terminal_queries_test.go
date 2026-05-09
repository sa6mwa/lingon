package session

import (
	"io"
	"os"
	"testing"

	"pkt.systems/lingon/internal/terminal"
)

func captureTerminalReplies(t *testing.T, session *localSession, fn func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = reader.Close()
	})

	session.ptyMu.Lock()
	session.pty = writer
	session.tty = nil
	session.ptyMu.Unlock()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read replies: %v", err)
	}
	return string(got)
}

func TestRespondToTerminalQueriesCodexStartupProbeUsesXtermFallback(t *testing.T) {
	session := &localSession{
		cursorQuery: func(terminal.Snapshot) (int, int, bool) {
			return 9, 4, true
		},
	}

	got := captureTerminalReplies(t, session, func() {
		session.respondToTerminalQueries([]byte("\x1b[>7u\x1b[?1004h\x1b[6n\x1b[?u\x1b[c"), terminal.Snapshot{})
	})
	want := "\x1b[9;4R\x1b[?1;2c"

	if got != want {
		t.Fatalf("expected xterm fallback replies %q, got %q", want, got)
	}
}

func TestRespondToTerminalQueriesXtermStatusAndAttributes(t *testing.T) {
	session := &localSession{}
	snap := terminal.Snapshot{
		Cursor: terminal.Cursor{X: 2, Y: 3},
		Cols:   80,
		Rows:   24,
	}

	got := captureTerminalReplies(t, session, func() {
		session.respondToTerminalQueries([]byte("\x1b[5n\x1b[6n\x1b[?5n\x1b[?6n\x1b[c\x1b[>c"), snap)
	})
	want := "\x1b[0n\x1b[4;3R\x1b[?0n\x1b[?4;3R\x1b[?1;2c\x1b[>0;0;0c"

	if got != want {
		t.Fatalf("expected xterm status/device replies %q, got %q", want, got)
	}
}

func TestFilterOSCOutputRespondsToColorQueriesAndSuppressesQueryBytes(t *testing.T) {
	session := &localSession{}

	var filtered []byte
	got := captureTerminalReplies(t, session, func() {
		filtered = session.filterOSCOutput([]byte("a\x1b]10;?\x07b\x1b]11;?\x1b\\c\x1b]12;?\x07d"))
	})
	wantReplies := "\x1b]10;rgb:ffff/ffff/ffff\x07" +
		"\x1b]11;rgb:0000/0000/0000\x07" +
		"\x1b]12;rgb:ffff/ffff/ffff\x07"

	if string(filtered) != "abcd" {
		t.Fatalf("expected OSC query bytes to be suppressed from output, got %q", string(filtered))
	}
	if got != wantReplies {
		t.Fatalf("expected OSC color query replies %q, got %q", wantReplies, got)
	}
}

func TestFilterOSCOutputUsesCapturedOSCDefaults(t *testing.T) {
	session := &localSession{}
	session.setOscDefaults("rgb:aaaa/aaaa/aaaa", "rgb:1111/1111/1111", "rgb:2222/2222/2222")

	got := captureTerminalReplies(t, session, func() {
		filtered := session.filterOSCOutput([]byte("\x1b]10;?\x07\x1b]11;?\x07\x1b]12;?\x07"))
		if len(filtered) != 0 {
			t.Fatalf("expected OSC query bytes to be suppressed from output, got %q", string(filtered))
		}
	})
	want := "\x1b]10;rgb:aaaa/aaaa/aaaa\x07" +
		"\x1b]11;rgb:1111/1111/1111\x07" +
		"\x1b]12;rgb:2222/2222/2222\x07"

	if got != want {
		t.Fatalf("expected captured OSC defaults %q, got %q", want, got)
	}
}
