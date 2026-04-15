package session

import (
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/host"
	"pkt.systems/lingon/internal/protocolpb"
)

func TestRunnerPublishWallInactivityStatusUsesCurrentState(t *testing.T) {
	publisher := host.NewPublisher(host.PublishOptions{SessionID: "s1"})
	var got *protocolpb.Frame
	publisher.OnFrame = func(frame *protocolpb.Frame) {
		got = frame
	}

	r := &Runner{
		clock:         clock.New(),
		localSessions: map[string]*localSession{"s1": {id: "s1", publisher: publisher}},
	}
	r.configureLocalWallNotification("s1", 2*time.Minute, "2m")

	r.publishWallInactivityStatus("s1", "")

	if got == nil {
		t.Fatal("expected wall inactivity status frame")
	}
	status := got.GetWallInactivityStatus()
	if status == nil {
		t.Fatalf("expected wall inactivity status payload, got %#v", got.Payload)
	}
	if !status.GetEnabled() {
		t.Fatalf("expected enabled status, got %+v", status)
	}
	if got := status.GetInactiveAfter(); got != "2m" {
		t.Fatalf("inactive_after = %q, want %q", got, "2m")
	}
}
