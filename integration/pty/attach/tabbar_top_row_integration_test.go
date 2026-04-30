//go:build integration
// +build integration

package integrationptyattach_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachTabBarHidesWhenCursorOnTopRow(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "attach-top-row",
		SessionName: "attach-top-row",
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)
	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"attach-top-row"})

	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "attach-top-row",
		Cols:      100,
		Rows:      30,
	})
	t.Cleanup(attach.Cancel)
	primeTabsByCount(t, attach, 1)

	attach.Eventually(6*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if hasConnectionStatusBanner(row) {
			return fmt.Errorf("waiting for connection banner to clear; row=%q", row)
		}
		if strings.Contains(row, "attach-top-row") {
			return fmt.Errorf("expected tab bar hidden while cursor owns top row; row=%q", row)
		}
		return nil
	})

	host.Send("echo TOPROW\n")
	attach.Eventually(6*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		cur := attach.Cursor()
		if cur.Row <= 1 {
			return fmt.Errorf("expected cursor below top row before clear; got row %d col %d", cur.Row, cur.Col)
		}
		row := screen.Row(0)
		if hasConnectionStatusBanner(row) {
			return fmt.Errorf("waiting for connection banner to clear; row=%q", row)
		}
		if !strings.Contains(row, "attach-top-row") {
			return fmt.Errorf("expected tab bar visible after leaving top row; row=%q", row)
		}
		return nil
	})

	host.Send("printf '\\033[H'\n")
	attach.Eventually(2*time.Second, 50*time.Millisecond, func(_ ptytest.Screen) error {
		cur := attach.Cursor()
		if cur.Row != 1 {
			return fmt.Errorf("expected cursor on row 1 after clear command; got row %d col %d", cur.Row, cur.Col)
		}
		return nil
	})

	attach.Wait(300 * time.Millisecond)
	row := attach.Screen().Row(0)
	if strings.Contains(row, "attach-top-row") {
		t.Fatalf("expected tab bar hidden when cursor is on top row; got %q", row)
	}
}
