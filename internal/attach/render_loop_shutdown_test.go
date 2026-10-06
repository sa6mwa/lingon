package attach

import (
	"context"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/testutil"
)

func TestStoppedRenderLoopIgnoresLateRequestsAndCanRestart(t *testing.T) {
	var output testutil.OutputBuffer
	client := &Client{Stdout: &output, Clock: clock.NewMock(), TermSize: func() (int, int) { return 2, 2 }}
	client.lastSnapshot = &protocolpb.Snapshot{Cols: 2, Rows: 2, Runes: []uint32{'a', 'b', 'c', 'd'}}
	stop := client.startRenderLoop(context.Background())
	client.requestRenderCurrent()
	stop()
	before := output.String()
	client.requestRenderCurrent() // models a late timer/frame notification
	if output.String() != before {
		t.Fatal("stopped render worker wrote after shutdown")
	}
	// A new connection can own a fresh worker; the old stop remains idempotent.
	stopNext := client.startRenderLoop(context.Background())
	stop()
	client.requestRenderCurrent()
	stopNext()
	before = output.String()
	client.requestRenderCurrent()
	if output.String() != before {
		t.Fatal("restarted worker wrote after its shutdown")
	}
}
