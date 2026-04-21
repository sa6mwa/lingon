package attach

import (
	"testing"

	"pkt.systems/lingon/internal/protocolpb"
)

func TestBuildHelloFrameIncludesReplaySeqDuringNormalResume(t *testing.T) {
	c := &Client{
		SessionID: "s1",
		ClientID:  "c1",
	}
	c.lastSeq = 42

	frame := c.buildHelloFrame(80, 24)
	hello := frame.GetHello()
	if hello == nil {
		t.Fatalf("expected hello payload")
	}
	if hello.GetLastSeq() != 42 {
		t.Fatalf("hello last_seq = %d, want 42", hello.GetLastSeq())
	}
}

func TestBuildHelloFrameClearsReplaySeqDuringGapResync(t *testing.T) {
	c := &Client{
		SessionID: "s1",
		ClientID:  "c1",
	}
	c.lastSeq = 42
	c.forceFreshHello = true

	frame := c.buildHelloFrame(80, 24)
	hello := frame.GetHello()
	if hello == nil {
		t.Fatalf("expected hello payload")
	}
	if hello.GetLastSeq() != 0 {
		t.Fatalf("gap resync hello last_seq = %d, want 0", hello.GetLastSeq())
	}
}

func TestHandleSessionsFrameAdvancesSequenceWithoutResync(t *testing.T) {
	c := &Client{
		SessionID: "s1",
		ClientID:  "c1",
	}
	c.lastSeq = 10

	var got []SessionInfo
	c.OnSessions = func(updated []SessionInfo) {
		got = updated
	}

	accept, resync := c.handleSessionsFrame(11, []*protocolpb.SessionInfo{{
		Id:   "alpha",
		Name: "Alpha",
	}})
	if !accept {
		t.Fatalf("sessions frame was not accepted")
	}
	if resync {
		t.Fatalf("sessions frame unexpectedly requested resync")
	}
	if c.lastSeq != 11 {
		t.Fatalf("lastSeq = %d, want 11", c.lastSeq)
	}
	if len(got) != 1 || got[0].ID != "alpha" {
		t.Fatalf("sessions callback = %+v, want alpha", got)
	}
}

func TestHandleSessionsFrameGapRequestsResync(t *testing.T) {
	c := &Client{
		SessionID: "s1",
		ClientID:  "c1",
	}
	c.lastSeq = 10

	accept, resync := c.handleSessionsFrame(12, nil)
	if !accept {
		t.Fatalf("gap sessions frame was not accepted")
	}
	if !resync {
		t.Fatalf("gap sessions frame did not request resync")
	}
	if !c.needsResync {
		t.Fatalf("needsResync = false, want true")
	}
	if c.lastSeq != 12 {
		t.Fatalf("lastSeq = %d, want 12", c.lastSeq)
	}
}
