package session

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/protocolpb"
)

func TestTabBarOverlayDoesNotFullRedrawOnPTYUpdates(t *testing.T) {
	cols, rows := 10, 4
	prev := makeSnapshot(cols, rows, 0, 0, 0, -1, -1)
	setRow(prev, 0, "AAAAAAAAAA")
	next := makeSnapshot(cols, rows, 0, 2, 'X', 0, 2)
	setRow(next, 0, "FLICKER!! ")

	r := New(Options{
		SessionID: "local",
		Cols:      cols,
		Rows:      rows,
	})
	r.compositor = mvu.NewRuntime()
	r.compositor.ApplyAction(mvu.SessionTabsAction{Input: mvu.SessionTabsInput{
		Sources: []mvu.SessionTabSource{
			{ID: "local", Name: "local"},
			{ID: "remote", Name: "remote"},
		},
		ActiveID: "local",
	}})
	r.renderCache.SetPrevSnapshot(prev)

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, stdoutR)
		close(done)
	}()

	if err := r.renderSnapshotWithOverlays(context.Background(), stdoutW, nil, next); err != nil {
		t.Fatalf("renderSnapshotWithOverlays: %v", err)
	}
	_ = stdoutW.Close()
	<-done

	if bytes.Contains(buf.Bytes(), []byte("FLICKER")) {
		t.Fatalf("expected tab bar compose to suppress base row-1 repaint while overlay is visible")
	}
}

func TestTabBarAutoHideClearsTopRow(t *testing.T) {
	cols, rows := 12, 4
	snap := makeSnapshot(cols, rows, 0, 0, 0, -1, -1)
	setRow(snap, 0, "PROMPT>   ")

	r := New(Options{
		SessionID: "local",
		Cols:      cols,
		Rows:      rows,
	})
	r.compositor = mvu.NewRuntime()
	r.compositor.ApplyAction(mvu.SessionTabsAction{Input: mvu.SessionTabsInput{
		Sources:  []mvu.SessionTabSource{{ID: "local", Name: "local"}},
		ActiveID: "local",
	}})
	r.renderCache.SetPrevSnapshot(snap)
	r.renderCache.Frame.LastTopOverlayVisible = true

	out := renderOnce(t, r, snap)
	if r.compositor.TabBarAutoHideDelay(time.Now()) != 0 {
		t.Fatalf("expected auto-hide delay to be cleared on top-row cursor")
	}
	if strings.Contains(out, "local") {
		t.Fatalf("expected tab bar hidden when cursor is on top row")
	}
	if !strings.Contains(out, "PROMPT") {
		t.Fatalf("expected top row to remain visible when tab bar hidden, got %q", out)
	}
}

func TestTabBarAutoHideAfterCtrlLCLear(t *testing.T) {
	cols, rows := 20, 6
	snap := makeSnapshot(cols, rows, 0, 0, 0, -1, -1)
	setRow(snap, 0, "PROMPT>            ")

	r := New(Options{
		SessionID: "local",
		Cols:      cols,
		Rows:      rows,
	})
	r.compositor = mvu.NewRuntime()
	r.compositor.ApplyAction(mvu.SessionTabsAction{Input: mvu.SessionTabsInput{
		Sources:  []mvu.SessionTabSource{{ID: "local", Name: "local"}},
		ActiveID: "local",
	}})
	r.renderCache.SetPrevSnapshot(snap)
	r.renderCache.Frame.LastTopOverlayVisible = true

	out := renderOnce(t, r, snap)
	if r.compositor.TabBarAutoHideDelay(time.Now()) != 0 {
		t.Fatalf("expected auto-hide delay to be cleared on top-row cursor")
	}
	if strings.Contains(out, "local") {
		t.Fatalf("expected tab bar hidden when cursor is on top row")
	}
	if !strings.Contains(out, "PROMPT") {
		t.Fatalf("expected top row to remain visible after ctrl+l clear, got %q", out)
	}
}

func TestConnectionBannerExpiresRedrawsTopRow(t *testing.T) {
	cols, rows := 12, 4
	snap := makeSnapshot(cols, rows, 0, 0, 0, -1, -1)
	setRow(snap, 0, "PROMPT>   ")
	clk := clock.NewMock()

	r := New(Options{
		SessionID: "local",
		Cols:      cols,
		Rows:      rows,
		Clock:     clk,
	})
	r.compositor = mvu.NewRuntime()
	r.compositor.ApplyAction(mvu.ContextAction{Input: mvu.ContextInput{Clock: clk}})
	r.compositor.ApplyAction(mvu.StatusAction{Input: mvu.StatusInput{
		Kind:     mvu.StatusConnected,
		Message:  "connected to https://example",
		Duration: 50 * time.Millisecond,
	}})
	r.renderCache.SetPrevSnapshot(snap)

	first := renderOnce(t, r, snap)
	if strings.Contains(first, "PROMPT") {
		t.Fatalf("expected connection banner to hide top-row prompt before expiry, got %q", first)
	}
	advanceClock(clk, 60*time.Millisecond)
	out := renderOnce(t, r, snap)

	if strings.Contains(out, "connected") {
		t.Fatalf("expected connection banner to be cleared after expiry, got %q", out)
	}
	if !strings.Contains(out, "PROMPT") {
		t.Fatalf("expected top row to be redrawn after connection banner expiry, got %q", out)
	}
}

func TestTabBarAutoHideDoesNotWaitForBannerExpiry(t *testing.T) {
	cols, rows := 40, 6
	snap := makeSnapshot(cols, rows, 0, 0, 0, -1, -1)
	snap.CursorVisible = true
	setRow(snap, 0, "PROMPT>                               ")

	r := New(Options{
		SessionID: "local",
		Cols:      cols,
		Rows:      rows,
	})
	r.compositor = mvu.NewRuntime()
	r.compositor.ApplyAction(mvu.SessionTabsAction{Input: mvu.SessionTabsInput{
		Sources:  []mvu.SessionTabSource{{ID: "local", Name: "local"}},
		ActiveID: "local",
	}})
	r.compositor.ApplyAction(mvu.StatusAction{Input: mvu.StatusInput{
		Kind:     mvu.StatusConnected,
		Message:  "connected to https://example",
		Duration: 3 * time.Second,
	}})
	r.renderCache.SetPrevSnapshot(snap)

	first := renderOnce(t, r, snap)
	if strings.Contains(first, "local") {
		t.Fatalf("expected tab bar hidden when cursor is on top row")
	}
	if !strings.Contains(first, "connected to https://example") {
		t.Fatalf("expected connection banner to render initially")
	}
}

func makeSnapshot(cols, rows, cursorX, cursorY int, ch rune, chCol, chRow int) *protocolpb.Snapshot {
	total := cols * rows
	runes := make([]uint32, total)
	if ch != 0 && chCol >= 0 && chRow >= 0 {
		idx := chRow*cols + chCol
		if idx >= 0 && idx < len(runes) {
			runes[idx] = uint32(ch)
		}
	}
	return &protocolpb.Snapshot{
		Cols:          uint32(cols),
		Rows:          uint32(rows),
		Runes:         runes,
		Cursor:        &protocolpb.Cursor{X: uint32(cursorX), Y: uint32(cursorY)},
		CursorVisible: true,
	}
}

func setRow(snap *protocolpb.Snapshot, row int, content string) {
	if snap == nil || row < 0 {
		return
	}
	cols := int(snap.Cols)
	if cols <= 0 || row >= int(snap.Rows) {
		return
	}
	for i := 0; i < cols; i++ {
		idx := row*cols + i
		if idx < 0 || idx >= len(snap.Runes) {
			continue
		}
		if i < len(content) {
			snap.Runes[idx] = uint32(content[i])
		} else {
			snap.Runes[idx] = uint32(' ')
		}
	}
}

func renderOnce(t *testing.T, r *Runner, snap *protocolpb.Snapshot) string {
	t.Helper()

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer func() {
		_ = stdoutR.Close()
	}()
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, stdoutR)
		close(done)
	}()

	cols, rows := int(snap.GetCols()), int(snap.GetRows())
	activeID, _ := r.activeSession()
	suppressTabs := r.tabSuppressed(activeID)
	if err := r.renderHostMVU(context.Background(), stdoutW, snap, cols, rows, false, suppressTabs); err != nil {
		t.Fatalf("renderHostMVU: %v", err)
	}
	_ = stdoutW.Close()
	<-done
	return buf.String()
}
