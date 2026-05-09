package publisher

import (
	"testing"

	"pkt.systems/lingon/internal/protocolpb"
)

func TestPublisherSendWallInactivityStatusEmitsFrame(t *testing.T) {
	publisher := New(Options{SessionID: "s1"})
	publisher.SetWallInactivityStatus(func() *protocolpb.WallInactivityStatus {
		return &protocolpb.WallInactivityStatus{
			Enabled:       true,
			InactiveAfter: "2m",
		}
	})

	var got *protocolpb.Frame
	publisher.OnFrame = func(frame *protocolpb.Frame) {
		got = frame
	}

	publisher.sendWallInactivityStatus()

	if got == nil {
		t.Fatal("expected wall inactivity status frame")
	}
	if got.GetSessionId() != "s1" {
		t.Fatalf("session id = %q, want %q", got.GetSessionId(), "s1")
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
