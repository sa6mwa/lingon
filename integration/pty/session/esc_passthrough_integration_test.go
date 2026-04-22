//go:build integration
// +build integration

package integrationptysession_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestHostEscKeyPassthroughHasNoDelay(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash required for ESC read regression")
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-esc-immediate",
		Shell:     "/bin/bash",
		Cols:      100,
		Rows:      30,
	})
	t.Cleanup(host.Cancel)

	host.Send("echo ESC_READY\n")
	waitForRawContains(t, host, "ESC_READY", 2*time.Second, 50*time.Millisecond, "expected shell ready marker")
	waitForRawIdle(t, host, 120*time.Millisecond, 2*time.Second)
	_ = host.DrainRaw()

	host.Send("echo ESC_WAIT; old=$(stty -g); stty -icanon -echo min 1 time 0; b=$(dd bs=1 count=1 2>/dev/null | od -An -tu1); stty \"$old\"; if [[ \"$b\" =~ 27 ]]; then echo ESC_OK; else echo ESC_FAIL; fi\n")
	waitForRawContains(t, host, "ESC_WAIT", 2*time.Second, 50*time.Millisecond, "expected ESC wait marker")
	host.SendBytes([]byte{0x1b})

	host.Eventually(1500*time.Millisecond, 25*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("ESC_OK") {
			return fmt.Errorf("waiting for ESC_OK")
		}
		return nil
	})
}

func TestHostRawSingleByteProbeWorks(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash required for single-byte probe")
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID: "host-byte-probe",
		Shell:     "/bin/bash",
		Cols:      100,
		Rows:      30,
	})
	t.Cleanup(host.Cancel)

	host.Send("echo BYTE_WAIT; old=$(stty -g); stty -icanon -echo min 1 time 0; b=$(dd bs=1 count=1 2>/dev/null | od -An -tu1); stty \"$old\"; if [[ \"$b\" =~ 120 ]]; then echo BYTE_OK; else echo BYTE_FAIL; fi\n")
	waitForRawContains(t, host, "BYTE_WAIT", 2*time.Second, 50*time.Millisecond, "expected BYTE wait marker")
	host.Send("x")

	host.Eventually(1500*time.Millisecond, 25*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("BYTE_OK") {
			return fmt.Errorf("waiting for BYTE_OK")
		}
		return nil
	})
}
