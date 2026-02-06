package attach_test

import (
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachFailsWithoutHost(t *testing.T) {
	h := newHarness(t)
	client := h.StartAttach(ptytest.AttachOptions{
		SessionID:      "session_test",
		ClientID:       "client1",
		RequestControl: true,
		Cols:           80,
		Rows:           24,
		NoHostTimeout:  2 * time.Second,
	})

	deadline := ptytest.Now(h.Clock()).Add(5 * time.Second)
	var (
		ok  bool
		err error
	)
	for ptytest.Now(h.Clock()).Before(deadline) {
		h.Advance(500 * time.Millisecond)
		ok, err = client.WaitErr(25 * time.Millisecond)
		if ok {
			break
		}
	}
	if !ok {
		t.Fatalf("attach hung without host")
	}
	if err == nil || !strings.Contains(err.Error(), "no host connected") {
		t.Fatalf("unexpected error: %v", err)
	}
}
