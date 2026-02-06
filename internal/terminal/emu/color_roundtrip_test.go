package emu

import (
	"bytes"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/protocol"
	"pkt.systems/lingon/internal/render"
)

func TestExplicit256ColorPreserved(t *testing.T) {
	e := New(1, 1)
	// Explicit 256-color index 7 (gray) should stay 38;5;7, not map to ANSI 37.
	if err := e.Write([]byte("\x1b[38;5;7mA")); err != nil {
		t.Fatalf("emulator write: %v", err)
	}
	snap, err := e.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	protoSnap := protocol.SnapshotToProto(snap)

	var buf bytes.Buffer
	if err := render.Snapshot(&buf, protoSnap); err != nil {
		t.Fatalf("render snapshot: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "37") {
		t.Fatalf("unexpected ANSI palette code; explicit 256-color should stay 38;5;7, got %q", out)
	}
	if !strings.Contains(out, "38;5;7") {
		t.Fatalf("expected explicit 256-color code, got %q", out)
	}
}
