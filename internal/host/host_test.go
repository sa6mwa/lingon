package host

import (
	"testing"

	"pkt.systems/lingon/internal/terminal/emu"
)

func TestEmulatorWrites(t *testing.T) {
	term := emu.New(10, 2)
	if err := term.Write([]byte("hi")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	snap, err := term.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.Cols != 10 || snap.Rows != 2 {
		t.Fatalf("size = %dx%d", snap.Cols, snap.Rows)
	}
}
