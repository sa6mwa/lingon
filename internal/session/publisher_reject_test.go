package session

import (
	"os"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/mvu"
)

func TestRelayRejectedStatusMessage(t *testing.T) {
	if got := relayRejectedStatusMessage(""); got != "session rejected by relay" {
		t.Fatalf("empty message: got %q", got)
	}
	if got := relayRejectedStatusMessage("  session already has active host "); got != "session rejected by relay: session already has active host" {
		t.Fatalf("message trim: got %q", got)
	}
}

func TestHandlePublisherSessionRejectedSetsOfflineAndErrorBanner(t *testing.T) {
	clk := clock.NewMock()
	runner := &Runner{
		opts:       Options{Endpoint: "https://relay.example"},
		clock:      clk,
		compositor: mvu.NewRuntime(),
		localSessions: map[string]*localSession{
			"session-a": {id: "session-a", name: "session-a"},
		},
		localOrder:  []string{"session-a"},
		localClosed: map[string]bool{},
	}
	runner.runtime().ApplyAction(mvu.ContextAction{Input: mvu.ContextInput{Clock: clk, Endpoint: "https://relay.example"}})
	runner.setActiveSession("session-a", true)

	_, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = stdoutW.Close()
	})

	local := runner.localSession("session-a")
	if local == nil {
		t.Fatalf("missing local session")
	}
	if local.Offline() {
		t.Fatalf("local session unexpectedly offline before rejection")
	}

	runner.handlePublisherSessionRejected(local, "session already has active host", stdoutW)

	if !local.Offline() {
		t.Fatalf("expected local session to be forced offline on relay rejection")
	}
	state := runner.runtime().State()
	if state.ConnectionStyle != mvu.BannerRed {
		t.Fatalf("connection style = %v, want BannerRed", state.ConnectionStyle)
	}
	if !strings.Contains(state.ConnectionMessage, "session rejected by relay") {
		t.Fatalf("connection message missing reject prefix: %q", state.ConnectionMessage)
	}
	if !strings.Contains(state.ConnectionMessage, "active host") {
		t.Fatalf("connection message missing relay reason: %q", state.ConnectionMessage)
	}
}
