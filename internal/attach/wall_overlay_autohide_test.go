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

func TestWallOverlayPreservesScrollbackViewport(t *testing.T) {
	const cols, rows = 80, 8
	clk := clock.NewMock()
	var buf bytes.Buffer
	client := &Client{
		SessionID: "s1",
		Endpoint:  "https://example",
		Stdout:    &buf,
		Clock:     clk,
		TermSize: func() (int, int) {
			return cols, rows
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

	snap := attachWallTestSnapshot(cols, rows, "LIVE-END-TOKEN")
	buffer := mvu.NewProtoScrollbackBuffer(32)
	scrollRows := make([]*protocolpb.ScrollbackRow, 0, rows)
	for i := 0; i < rows; i++ {
		scrollRows = append(scrollRows, attachWallTestScrollbackRow(cols, "HISTORY-TOKEN"))
	}
	buffer.Apply(&protocolpb.Scrollback{Cols: uint32(cols), Rows: scrollRows})
	client.scrollbackBuffer = buffer
	client.scrollbackView.EnterAt(buffer.Len()+int(snap.Rows), rows, buffer.Len(), cols, cols, 0)

	client.renderSnapshot(snap)
	if out := buf.String(); !strings.Contains(out, "HISTORY-TOKEN") {
		t.Fatalf("expected initial scrollback render to show history, got %q", out)
	}
	buf.Reset()

	client.handleWall(&protocolpb.Wall{
		Sender:         "alice@127.0.0.1",
		Message:        "hello",
		TimeoutSeconds: 1,
	})
	out := buf.String()
	if strings.Contains(out, "LIVE-END-TOKEN") {
		t.Fatalf("wall modal repaint rendered live viewport instead of preserving scrollback viewport: %q", out)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("expected wall modal bytes, got %q", out)
	}
}

func TestWallOverlayPreservesMixedScrollbackAndLiveViewport(t *testing.T) {
	const cols, rows = 80, 8
	clk := clock.NewMock()
	var buf bytes.Buffer
	client := &Client{
		SessionID: "s1",
		Endpoint:  "https://example",
		Stdout:    &buf,
		Clock:     clk,
		TermSize: func() (int, int) {
			return cols, rows
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

	snap := attachWallTestSnapshot(cols, rows, "")
	attachWallTestSetSnapshotRow(snap, 0, "LIVE-VISIBLE-TOKEN")
	attachWallTestSetSnapshotRow(snap, rows-1, "LIVE-END-TOKEN")
	buffer := mvu.NewProtoScrollbackBuffer(32)
	scrollRows := make([]*protocolpb.ScrollbackRow, 0, rows/2)
	for i := 0; i < rows/2; i++ {
		scrollRows = append(scrollRows, attachWallTestScrollbackRow(cols, "HISTORY-TOKEN"))
	}
	buffer.Apply(&protocolpb.Scrollback{Cols: uint32(cols), Rows: scrollRows})
	client.scrollbackBuffer = buffer
	client.scrollbackView.EnterAt(buffer.Len()+int(snap.Rows), rows, buffer.Len(), cols, cols, 0)

	client.renderSnapshot(snap)
	initial := buf.String()
	if !strings.Contains(initial, "HISTORY-TOKEN") {
		t.Fatalf("expected mixed viewport to include history, got %q", initial)
	}
	if !strings.Contains(initial, "LIVE-VISIBLE-TOKEN") {
		t.Fatalf("expected mixed viewport to include visible live rows, got %q", initial)
	}
	buf.Reset()

	client.handleWall(&protocolpb.Wall{
		Sender:         "alice@127.0.0.1",
		Message:        "hello mixed",
		TimeoutSeconds: 1,
	})
	out := buf.String()
	if strings.Contains(out, "LIVE-END-TOKEN") {
		t.Fatalf("wall modal repaint rendered outside the mixed viewport into live end content: %q", out)
	}
	if !strings.Contains(out, "hello mixed") {
		t.Fatalf("expected wall modal bytes, got %q", out)
	}
}

func attachWallTestSnapshot(cols, rows int, liveToken string) *protocolpb.Snapshot {
	snap := &protocolpb.Snapshot{
		Cols:          uint32(cols),
		Rows:          uint32(rows),
		Runes:         make([]uint32, cols*rows),
		Modes:         make([]int32, cols*rows),
		Fg:            make([]uint32, cols*rows),
		Bg:            make([]uint32, cols*rows),
		Cursor:        &protocolpb.Cursor{X: 0, Y: uint32(rows - 1)},
		CursorVisible: true,
	}
	attachWallTestSetSnapshotRow(snap, 0, liveToken)
	return snap
}

func attachWallTestSetSnapshotRow(snap *protocolpb.Snapshot, row int, content string) {
	if snap == nil || row < 0 || row >= int(snap.Rows) {
		return
	}
	cols := int(snap.Cols)
	for i := 0; i < cols; i++ {
		r := ' '
		if i < len(content) {
			r = rune(content[i])
		}
		snap.Runes[row*cols+i] = uint32(r)
	}
}

func attachWallTestScrollbackRow(cols int, content string) *protocolpb.ScrollbackRow {
	row := &protocolpb.ScrollbackRow{
		Runes: make([]uint32, cols),
		Modes: make([]int32, cols),
		Fg:    make([]uint32, cols),
		Bg:    make([]uint32, cols),
	}
	for i := 0; i < cols; i++ {
		r := ' '
		if i < len(content) {
			r = rune(content[i])
		}
		row.Runes[i] = uint32(r)
	}
	return row
}
