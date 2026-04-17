package attach

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal/emu"
)

func TestHandleRoutedHeadlessStatus(t *testing.T) {
	client := &Client{
		Endpoint: "local://headless",
	}
	client.runCtx = context.Background()
	client.effects = mvu.NewEffectScheduler(client.clock())
	defer client.effects.StopAll()

	if ok := client.handleRoutedHeadlessStatus(&protocolpb.Wall{
		Sender:  headless.RoutedStatusSenderLost,
		Message: "connection lost to https://relay.example/v1, reconnecting",
	}); !ok {
		t.Fatalf("expected lost status wall to be handled")
	}
	state := client.ensureCompositor().State()
	if !strings.Contains(state.ConnectionMessage, "connection lost") {
		t.Fatalf("expected connection-lost banner, got %q", state.ConnectionMessage)
	}
	if state.ConnectionStyle != mvu.BannerRed {
		t.Fatalf("expected red connection banner, got %v", state.ConnectionStyle)
	}

	if ok := client.handleRoutedHeadlessStatus(&protocolpb.Wall{
		Sender:         headless.RoutedStatusSenderBackoff,
		Message:        "connection lost to https://relay.example/v1, reconnecting in 4s",
		TimeoutSeconds: 4,
	}); !ok {
		t.Fatalf("expected backoff status wall to be handled")
	}
	state = client.ensureCompositor().State()
	if !strings.Contains(state.ConnectionMessage, "reconnecting in 4s") {
		t.Fatalf("expected reconnect countdown banner, got %q", state.ConnectionMessage)
	}

	if ok := client.handleRoutedHeadlessStatus(&protocolpb.Wall{
		Sender:         headless.RoutedStatusSenderConnected,
		Message:        "connected to https://relay.example/v1",
		TimeoutSeconds: 1,
	}); !ok {
		t.Fatalf("expected connected status wall to be handled")
	}
	state = client.ensureCompositor().State()
	if !strings.Contains(state.ConnectionMessage, "connected to") {
		t.Fatalf("expected connected banner, got %q", state.ConnectionMessage)
	}
	if state.ConnectionStyle != mvu.BannerGreen {
		t.Fatalf("expected green connection banner, got %v", state.ConnectionStyle)
	}
	if state.ConnectionExpiresAt.IsZero() {
		t.Fatalf("expected connected banner to have expiry")
	}

	if ok := client.handleRoutedHeadlessStatus(&protocolpb.Wall{
		Sender:         headless.RoutedStatusSenderInfo,
		Message:        "wall inactivity 2m",
		TimeoutSeconds: 2,
	}); !ok {
		t.Fatalf("expected info status wall to be handled")
	}
	state = client.ensureCompositor().State()
	if !strings.Contains(state.ConnectionMessage, "wall inactivity 2m") {
		t.Fatalf("expected info status banner message, got %q", state.ConnectionMessage)
	}
	if state.ConnectionStyle != mvu.BannerGreen {
		t.Fatalf("expected info status banner style to be green, got %v", state.ConnectionStyle)
	}

	if ok := client.handleRoutedHeadlessStatus(&protocolpb.Wall{
		Sender:         headless.RoutedStatusSenderError,
		Message:        "wall inactivity toggle failed",
		TimeoutSeconds: 2,
	}); !ok {
		t.Fatalf("expected error status wall to be handled")
	}
	state = client.ensureCompositor().State()
	if !strings.Contains(state.ConnectionMessage, "wall inactivity toggle failed") {
		t.Fatalf("expected error status banner message, got %q", state.ConnectionMessage)
	}
	if state.ConnectionStyle != mvu.BannerRed {
		t.Fatalf("expected error status banner style to be red, got %v", state.ConnectionStyle)
	}

	if ok := client.handleRoutedHeadlessStatus(&protocolpb.Wall{
		Sender:  "user-wall",
		Message: "hello",
	}); ok {
		t.Fatalf("expected non-routed wall sender to be ignored")
	}
}

func TestHandleRoutedHeadlessStatusBackoffWithoutTimeout(t *testing.T) {
	client := &Client{
		Endpoint: "local://headless",
	}
	client.runCtx = context.Background()
	client.effects = mvu.NewEffectScheduler(client.clock())
	defer client.effects.StopAll()

	client.handleRoutedHeadlessStatus(&protocolpb.Wall{
		Sender:  headless.RoutedStatusSenderBackoff,
		Message: "connection lost to https://relay.example/v1, reconnecting in 2s",
	})
	state := client.ensureCompositor().State()
	if !strings.Contains(state.ConnectionMessage, "reconnecting in 2s") {
		t.Fatalf("unexpected backoff banner: %q", state.ConnectionMessage)
	}
	if !state.ConnectionExpiresAt.IsZero() {
		t.Fatalf("expected backoff banner to remain persistent")
	}

	// Ensure the banner remains visible without countdown-expiry side effects.
	time.Sleep(10 * time.Millisecond)
	state = client.ensureCompositor().State()
	if state.ConnectionMessage == "" {
		t.Fatalf("expected backoff banner to remain visible")
	}
}

func TestHandleWallInactivityStatus(t *testing.T) {
	client := &Client{
		Endpoint: "https://relay.example",
	}
	client.runCtx = context.Background()
	client.effects = mvu.NewEffectScheduler(client.clock())
	defer client.effects.StopAll()

	client.handleWallInactivityStatus(&protocolpb.WallInactivityStatus{
		Enabled:       true,
		InactiveAfter: "2m",
	})
	state := client.ensureCompositor().State()
	if !strings.Contains(state.ConnectionMessage, "wall inactivity 2m") {
		t.Fatalf("expected enabled wall inactivity banner, got %q", state.ConnectionMessage)
	}
	if state.ConnectionStyle != mvu.BannerGreen {
		t.Fatalf("expected green wall inactivity banner, got %v", state.ConnectionStyle)
	}

	client.handleWallInactivityStatus(&protocolpb.WallInactivityStatus{})
	state = client.ensureCompositor().State()
	if !strings.Contains(state.ConnectionMessage, "wall inactivity off") {
		t.Fatalf("expected disabled wall inactivity banner, got %q", state.ConnectionMessage)
	}

	client.handleWallInactivityStatus(&protocolpb.WallInactivityStatus{Error: "wall inactivity toggle failed"})
	state = client.ensureCompositor().State()
	if !strings.Contains(state.ConnectionMessage, "wall inactivity toggle failed") {
		t.Fatalf("expected error wall inactivity banner, got %q", state.ConnectionMessage)
	}
	if state.ConnectionStyle != mvu.BannerRed {
		t.Fatalf("expected red wall inactivity error banner, got %v", state.ConnectionStyle)
	}
}

func TestPrepareForCtrlLClearStopsPendingRedrawEffects(t *testing.T) {
	clk := clock.NewMock()
	var buf bytes.Buffer
	client := &Client{
		Clock:  clk,
		Stdout: &buf,
		TermSize: func() (int, int) {
			return 80, 24
		},
	}
	client.runCtx = context.Background()
	client.effects = mvu.NewEffectScheduler(clk)
	defer client.effects.StopAll()

	tabFired := false
	stateFired := false
	client.effects.Schedule(client.runCtx, mvu.EffectKeyTabAutoHide, time.Second, func() {
		tabFired = true
	})
	client.effects.Schedule(client.runCtx, mvu.EffectKeyStateExpiry, time.Second, func() {
		stateFired = true
	})

	client.PrepareForCtrlLClear()
	clk.Add(2 * time.Second)

	if tabFired {
		t.Fatalf("expected tab auto-hide effect to be canceled")
	}
	if stateFired {
		t.Fatalf("expected state-expiry effect to be canceled")
	}
	client.renderMu.Lock()
	forceClear := client.forceClear
	client.renderMu.Unlock()
	if forceClear {
		t.Fatalf("expected immediate render to consume forceClear")
	}
}

func TestPrepareForCtrlLClearRendersImmediatelyAndAvoidsDeferredFullClear(t *testing.T) {
	clk := clock.NewMock()
	var buf bytes.Buffer
	client := &Client{
		SessionID: "s1",
		Endpoint:  "https://example",
		Clock:     clk,
		Stdout:    &buf,
		TermSize: func() (int, int) {
			return 80, 24
		},
		compositor: mvu.NewRuntime(),
	}
	client.runCtx = context.Background()
	client.effects = mvu.NewEffectScheduler(clk)
	defer client.effects.StopAll()
	client.compositor.ApplyAction(mvu.ContextAction{Input: mvu.ContextInput{
		Clock:     clk,
		SessionID: client.SessionID,
		Endpoint:  client.Endpoint,
	}})

	snap := &protocolpb.Snapshot{
		Cols:          80,
		Rows:          24,
		Runes:         make([]uint32, 80*24),
		Modes:         make([]int32, 80*24),
		Fg:            make([]uint32, 80*24),
		Bg:            make([]uint32, 80*24),
		Cursor:        &protocolpb.Cursor{X: 0, Y: 1},
		CursorVisible: true,
	}

	client.renderSnapshot(snap)
	client.showInfoStatus("wall inactivity: on")
	buf.Reset()

	client.PrepareForCtrlLClear()
	if buf.Len() == 0 {
		t.Fatalf("expected immediate redraw during Ctrl+L clear")
	}
	if !strings.Contains(buf.String(), "\x1b[2J\x1b[H") {
		t.Fatalf("expected immediate Ctrl+L redraw to clear the viewport")
	}

	buf.Reset()
	client.RenderCurrent()
	if strings.Contains(buf.String(), "\x1b[2J\x1b[H") {
		t.Fatalf("expected subsequent unrelated redraw to avoid deferred full clear")
	}
	client.renderMu.Lock()
	forceClear := client.forceClear
	client.renderMu.Unlock()
	if forceClear {
		t.Fatalf("expected immediate redraw to consume forceClear")
	}
}

func TestPrepareForCtrlLClearPreservesStateExpiryWithoutRemoteSnapshot(t *testing.T) {
	clk := clock.NewMock()
	var buf bytes.Buffer
	client := &Client{
		SessionID: "s1",
		Endpoint:  "https://example",
		Clock:     clk,
		Stdout:    &buf,
		TermSize: func() (int, int) {
			return 80, 24
		},
		compositor: mvu.NewRuntime(),
	}
	client.runCtx = context.Background()
	client.effects = mvu.NewEffectScheduler(clk)
	defer client.effects.StopAll()
	expired := make(chan struct{}, 1)
	client.OnOverlayStateChange = func() {
		select {
		case expired <- struct{}{}:
		default:
		}
	}
	client.compositor.ApplyAction(mvu.ContextAction{Input: mvu.ContextInput{
		Clock:     clk,
		SessionID: client.SessionID,
		Endpoint:  client.Endpoint,
	}})

	snap := &protocolpb.Snapshot{
		Cols:          80,
		Rows:          24,
		Runes:         make([]uint32, 80*24),
		Modes:         make([]int32, 80*24),
		Fg:            make([]uint32, 80*24),
		Bg:            make([]uint32, 80*24),
		Cursor:        &protocolpb.Cursor{X: 0, Y: 1},
		CursorVisible: true,
	}

	client.renderSnapshot(snap)
	client.showInfoStatus("wall inactivity: on")
	buf.Reset()

	client.PrepareForCtrlLClear()
	buf.Reset()

	clk.Add(2100 * time.Millisecond)
	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		if len(expired) > 0 && buf.Len() > 0 {
			<-expired
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected expiry redraw after Ctrl+L clear")
		}
		runtime.Gosched()
	}
	e := emu.New(80, 24)
	if err := e.Write(buf.Bytes()); err != nil {
		t.Fatalf("emulator write: %v", err)
	}
	screen, err := e.Snapshot()
	if err != nil {
		t.Fatalf("emulator snapshot: %v", err)
	}
	var rows []string
	for y := 0; y < screen.Rows; y++ {
		row := make([]rune, 0, screen.Cols)
		for x := 0; x < screen.Cols; x++ {
			cell := screen.Cells[y*screen.Cols+x]
			if cell.Rune == 0 {
				row = append(row, ' ')
				continue
			}
			row = append(row, cell.Rune)
		}
		rows = append(rows, string(row))
	}
	joined := strings.Join(rows, "\n")
	if strings.Contains(joined, "wall inactivity: on") {
		t.Fatalf("expected expiring overlay to disappear after Ctrl+L clear redraw, got:\n%s", joined)
	}
	if strings.Contains(buf.String(), "\x1b[2J\x1b[H") {
		t.Fatalf("expected expiry redraw without deferred full clear")
	}
	if screen.Rows != 24 {
		t.Fatalf("unexpected emulator state: %s", fmt.Sprintf("%dx%d", screen.Cols, screen.Rows))
	}
}
