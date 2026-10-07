//go:build integration

package integrationptyattach_test

import (
	"fmt"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestRenameCtrlCCancellationPreservesHostAndAttachInput(t *testing.T) {
	for _, mode := range []string{"host", "attach"} {
		for _, input := range []string{"Ohello", "[hello"} {
			t.Run(mode+"/"+input, func(t *testing.T) {
				clk := clock.NewMock()
				h := newHarness(t, ptytest.WithClock(clk))
				id := "rename-cancel"
				host := h.StartHost(ptytest.HostOptions{
					SessionID: id, SessionName: id, Shell: fixedPromptEmitRowsBash(t), Cols: 100, Rows: 24,
				})
				t.Cleanup(host.Cancel)
				waitForSessions(t, clk, h.Endpoint(), h.AccessToken(), []string{id})
				terminal := host
				if mode == "attach" {
					terminal = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: id, Cols: 100, Rows: 24})
					t.Cleanup(terminal.Cancel)
				}
				waitForPromptVisible(t, terminal, 5*time.Second)
				terminal.Send("printf 'CANCEL_%s\\n' READY\r")
				terminal.Eventually(5*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
					if !screen.Contains("CANCEL_READY") {
						return fmt.Errorf("shell input/output not ready: %s", screen.String())
					}
					return nil
				})
				terminal.Send("\x0ce")
				terminal.Eventually(5*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
					if !screen.Contains("Rename session") {
						return fmt.Errorf("rename prompt not active: %s", screen.String())
					}
					return nil
				})
				terminal.Send("\x03" + input)
				terminal.Eventually(5*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
					if screen.Contains("Rename session") || !screen.Contains("PROMPT>"+input) {
						return fmt.Errorf("cancelled rename lost terminal input %q: %s", input, screen.String())
					}
					return nil
				})
			})
		}
	}
}
