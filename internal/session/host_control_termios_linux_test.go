//go:build linux

package session

import (
	"testing"

	"github.com/creack/pty"

	"pkt.systems/lingon/internal/publisher"
)

func TestRepeatedHostControlPreservesShellEOFSetting(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = slave.Close(); _ = master.Close() })
	local := &localSession{pty: master, holderID: publisher.HostControlID, veofOrig: 4, veofSet: true}
	// A child owns its terminal configuration while the host already owns input.
	if err := setVEOF(slave, 17); err != nil {
		t.Fatal(err)
	}
	local.setHolder(publisher.HostControlID)
	if got, err := getVEOF(slave); err != nil || got != 17 {
		t.Fatalf("redundant host ownership rewrote shell EOF: got %d, err %v; want 17", got, err)
	}
	local.setHolder("remote")
	if got, err := getVEOF(slave); err != nil || got != 0 {
		t.Fatalf("remote ownership EOF: got %d, err %v; want 0", got, err)
	}
	local.setHolder(publisher.HostControlID)
	if got, err := getVEOF(slave); err != nil || got != 4 {
		t.Fatalf("host takeover EOF: got %d, err %v; want 4", got, err)
	}
}
