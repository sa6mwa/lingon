package attach

import (
	"context"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/protocolpb"
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

func TestPrepareForCtrlLClearStopsPendingRedrawEffects(t *testing.T) {
	clk := clock.NewMock()
	client := &Client{}
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
	if !forceClear {
		t.Fatalf("expected forceClear to remain armed")
	}
}
