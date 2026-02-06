package attach

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/protocolpb"
)

func TestWallAutoHideDoesNotForceFullRedraw(t *testing.T) {
	clk := clock.NewMock()
	var buf bytes.Buffer
	client := &Client{
		SessionID: "s1",
		Endpoint:  "https://example",
		Stdout:    &buf,
		Clock:     clk,
		TermSize: func() (int, int) {
			return 80, 24
		},
		compositor: mvu.NewRuntime(),
	}
	client.compositor.ApplyAction(mvu.ContextAction{Input: mvu.ContextInput{
		SessionID: client.SessionID,
		Endpoint:  client.Endpoint,
	}})
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.runCtx = runCtx

	snap := &protocolpb.Snapshot{
		Cols:          80,
		Rows:          24,
		Runes:         make([]uint32, 80*24),
		Modes:         make([]int32, 80*24),
		Fg:            make([]uint32, 80*24),
		Bg:            make([]uint32, 80*24),
		Cursor:        &protocolpb.Cursor{X: 0, Y: 0},
		CursorVisible: true,
	}
	client.renderSnapshot(snap)
	buf.Reset()

	client.handleWall(&protocolpb.Wall{
		Sender:         "alice@127.0.0.1",
		Message:        "hello",
		TimeoutSeconds: 1,
	})
	buf.Reset()
	clk.Add(1100 * time.Millisecond)
	time.Sleep(10 * time.Millisecond)

	out := buf.String()
	if strings.Contains(out, "\x1b[2J\x1b[H") {
		t.Fatalf("expected wall auto-hide redraw without full clear")
	}
}
