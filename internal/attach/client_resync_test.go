package attach

import "testing"

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
