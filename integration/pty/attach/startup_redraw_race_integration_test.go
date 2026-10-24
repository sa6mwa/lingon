//go:build integration

package integrationptyattach_test

import (
	"fmt"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/ptytest"
)

func TestMultiAttachStartupRedrawPreservesTerminalInput(t *testing.T) {
	h := newHarness(t)
	const sessionID = "startup-redraw"
	host := h.StartHost(ptytest.HostOptions{
		SessionID: sessionID,
		Shell:     writeAttachPromptShell(t),
		Cols:      80,
		Rows:      24,
	})
	t.Cleanup(host.Cancel)
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{sessionID})

	redrawDone := make(chan struct{})
	sess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: sessionID,
		Cols:      80,
		Rows:      24,
		OnView: func(_ string, client *attach.Client) {
			primed := make(chan struct{})
			go func() {
				defer close(redrawDone)
				// The owner may render a view before its detached transport
				// starts. Keep rendering across that startup boundary.
				client.RenderCurrent()
				close(primed)
				for range 2000 {
					client.RenderCurrent()
				}
			}()
			<-primed
		},
	})
	t.Cleanup(sess.Cancel)
	<-redrawDone
	waitForPromptVisible(t, sess, 8*time.Second)
	sess.Send("printf 'STARTUP-%s\\n' READY\r")
	sess.Eventually(8*time.Second, 20*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("STARTUP-READY") {
			return fmt.Errorf("terminal did not accept input after concurrent startup redraws: %s", screen.String())
		}
		return nil
	})
}
