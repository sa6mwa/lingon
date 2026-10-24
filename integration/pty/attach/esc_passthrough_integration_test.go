//go:build integration
// +build integration

package integrationptyattach_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachSingleEscKeyPassthrough(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash required for ESC read regression")
	}

	h := newHarness(t)
	sessionID := "attach-single-esc"
	h.StartHost(ptytest.HostOptions{
		SessionID: sessionID,
		Shell:     "/bin/bash",
		Cols:      100,
		Rows:      30,
	})
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionID})

	attach := h.StartAttach(ptytest.AttachOptions{
		SessionID:      sessionID,
		RequestControl: true,
		Cols:           100,
		Rows:           30,
	})
	t.Cleanup(attach.Cancel)
	waitForClientCount(t, h, sessionID, 1, 3*time.Second)
	assertAttachSingleEscPassthrough(t, attach)
}

func TestAttachMultiEscKeyPassthrough(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash required for ESC read regression")
	}

	h := newHarness(t)
	sessionID := "attach-multi-esc"
	h.StartHost(ptytest.HostOptions{
		SessionID: sessionID,
		Shell:     "/bin/bash",
		Cols:      100,
		Rows:      30,
	})
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionID})

	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: sessionID,
		Cols:      100,
		Rows:      30,
	})
	t.Cleanup(attach.Cancel)
	waitForClientCount(t, h, sessionID, 1, 3*time.Second)
	assertAttachSingleEscPassthrough(t, attach)
}

func assertAttachSingleEscPassthrough(t *testing.T, attach *ptytest.PTYSession) {
	t.Helper()
	// Construct output markers from octal escapes so the local echo of this
	// command cannot satisfy the readiness or result assertions.
	attach.Send("ready=$(printf '\\105\\123\\103\\137\\127\\101\\111\\124'); ok=$(printf '\\105\\123\\103\\137\\117\\113'); fail=$(printf '\\105\\123\\103\\137\\106\\101\\111\\114'); old=$(stty -g); stty -icanon -echo min 1 time 0; printf '%s\\n' \"$ready\"; b=$(dd bs=1 count=1 2>/dev/null | od -An -tu1); stty \"$old\"; if [[ \"$b\" =~ 27 ]]; then printf '%s\\n' \"$ok\"; else printf '%s\\n' \"$fail\"; fi\n")
	if !waitForRawContains(t, attach, "ESC_WAIT", 2*time.Second) {
		t.Fatalf("expected ESC wait marker")
	}
	attach.SendBytes([]byte{0x1b})

	attach.Eventually(1500*time.Millisecond, 25*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("ESC_OK") {
			return fmt.Errorf("waiting for ESC_OK")
		}
		return nil
	})
}
