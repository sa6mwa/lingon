//go:build integration
// +build integration

package integrationptyattach_test

import (
	"fmt"
	"regexp"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/relay"
)

func TestAttachShareTokenHelpModalClosesOnQAndQOnly(t *testing.T) {
	h := newHarness(t)

	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-help-share",
		SessionName: "host-help-share",
		Cols:        80,
		Rows:        24,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"host-help-share"})

	token := h.CreateShareToken("host-help-share", relay.ShareScopeView, time.Minute)
	attach := h.StartAttach(ptytest.AttachOptions{
		SessionID:  "host-help-share",
		ShareToken: token,
		Cols:       80,
		Rows:       24,
		RawInput:   true,
	})
	t.Cleanup(attach.Cancel)

	helpVisibleRe := regexp.MustCompile(`session:\s+host-help-share`)
	helpControlRe := regexp.MustCompile(`Ctrl\+L\s+h\s+help`)

	showHelp := func() {
		attach.SendBytes([]byte{0x0c, 'h'})
		attach.Eventually(3*time.Second, 20*time.Millisecond, func(screen ptytest.Screen) error {
			if screen.Match(helpVisibleRe) && screen.Match(helpControlRe) {
				return nil
			}
			return fmt.Errorf("help not visible:\n%s", screen.String())
		})
	}
	waitForHelpHidden := func() {
		attach.Eventually(3*time.Second, 20*time.Millisecond, func(screen ptytest.Screen) error {
			if !screen.Match(helpVisibleRe) {
				return nil
			}
			return fmt.Errorf("help still visible:\n%s", screen.String())
		})
	}

	showHelp()
	// PTY input can be line-buffered in tests; send a newline to flush q.
	attach.SendBytes([]byte{'q', '\n'})
	waitForHelpHidden()

	showHelp()
	attach.SendBytes([]byte{'Q'})
	waitForHelpHidden()
}
