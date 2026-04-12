package attach_test

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachTabSwitchUsesReplaySeqAfterServerRestart(t *testing.T) {
	recorder := ptytest.NewWSRecorder()
	h := newHarness(t, ptytest.WithWSRecorder(recorder))

	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:   "replay-cache-a",
		SessionName: "ra",
		Shell:       "/bin/sh",
		Cols:        80,
		Rows:        24,
	})
	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "replay-cache-b",
		SessionName: "rb",
		Shell:       "/bin/sh",
		Cols:        80,
		Rows:        24,
	})
	t.Cleanup(hostA.Cancel)
	t.Cleanup(hostB.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"replay-cache-a", "replay-cache-b"})

	hostA.Send("echo RA_BASELINE\n")
	if !screenContainsWithin(hostA, "RA_BASELINE", 2*time.Second) {
		t.Fatalf("expected baseline output on host A")
	}
	hostB.Send("echo RB_BASELINE\n")
	if !screenContainsWithin(hostB, "RB_BASELINE", 2*time.Second) {
		t.Fatalf("expected baseline output on host B")
	}

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "replay-cache-a",
		Cols:      80,
		Rows:      24,
	})
	t.Cleanup(attachSess.Cancel)
	waitForTabLabels(t, attachSess, []string{"ra", "rb"}, 6*time.Second)
	primeTabsByCount(t, attachSess, 2)

	if !screenContainsWithin(attachSess, "RA_BASELINE", 2*time.Second) {
		t.Fatalf("expected baseline output before switch")
	}

	attachSess.SendCtrlL()
	attachSess.Send("n")
	attachSess.Wait(200 * time.Millisecond)

	attachSess.Send("echo RB_SWITCH\n")
	if !screenContainsWithin(attachSess, "RB_SWITCH", 3*time.Second) {
		t.Fatalf("expected RB_SWITCH output after switch to B")
	}

	framesBeforeRestart := len(recorder.Frames())
	h.StopServer()
	h.RestartServer()

	waitForClientCount(t, h, "replay-cache-a", 1, 6*time.Second)
	waitForClientCount(t, h, "replay-cache-b", 1, 6*time.Second)

	waitForFramePayload(t, h.Clock(), recorder, "client", "replay-cache-a", ptytest.DirClientToServer, "hello", 2, 10*time.Second)
	waitForFramePayload(t, h.Clock(), recorder, "client", "replay-cache-b", ptytest.DirClientToServer, "hello", 2, 10*time.Second)

	var aHelloSeq uint64
	var bHelloSeq uint64
	afterRestart := recorder.Frames()[framesBeforeRestart:]
	for _, rec := range afterRestart {
		if rec.Direction != ptytest.DirClientToServer || rec.Payload != "hello" {
			continue
		}
		switch rec.SessionID {
		case "replay-cache-a":
			aHelloSeq = parseHelloLastSeq(rec.Raw)
		case "replay-cache-b":
			bHelloSeq = parseHelloLastSeq(rec.Raw)
		}
	}
	if aHelloSeq == 0 {
		t.Fatalf("expected replay-cache-a hello last_seq >0 after restart, got %d", aHelloSeq)
	}
	if bHelloSeq == 0 {
		t.Fatalf("expected replay-cache-b hello last_seq >0 after restart, got %d", bHelloSeq)
	}

	assertNoTabSwitchFlickerAfterAction(t, attachSess, 24, 800*time.Millisecond, func() {
		attachSess.SendCtrlL()
		attachSess.Send("p")
	})
	attachSess.Send("echo RA_SWITCHBACK\n")
	if !screenContainsWithin(attachSess, "RA_SWITCHBACK", 3*time.Second) {
		t.Fatalf("expected switch back to RA_SWITCHBACK after restart")
	}

	hostB.Send("echo RB_AFTER_RESTART\n")
	attachSess.Wait(120 * time.Millisecond)
	if !screenContainsWithin(hostB, "RB_AFTER_RESTART", 3*time.Second) {
		t.Fatalf("expected remote output after restart on host B")
	}
}

func parseHelloLastSeq(raw []byte) uint64 {
	var frame protocolpb.Frame
	if err := proto.Unmarshal(raw, &frame); err != nil {
		return 0
	}
	hello := frame.GetHello()
	if hello == nil {
		return 0
	}
	return hello.GetLastSeq()
}
