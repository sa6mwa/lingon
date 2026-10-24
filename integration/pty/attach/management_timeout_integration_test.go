//go:build integration

package integrationptyattach_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachManagementRefreshTimeoutAllowsNextRename(t *testing.T) {
	clk := clock.NewMock()
	h := newHarness(t, ptytest.WithClock(clk))
	host := h.StartHost(ptytest.HostOptions{SessionID: "management-timeout", SessionName: "management-timeout", Shell: "/bin/sh", Cols: 100, Rows: 24})
	t.Cleanup(host.Cancel)
	waitForSessions(t, clk, h.Endpoint(), h.AccessToken(), []string{"management-timeout"})
	var stall atomic.Bool
	var active atomic.Pointer[attach.Client]
	started := make(chan struct{})
	terminal := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "management-timeout", Cols: 100, Rows: 24, AccessToken: h.AccessToken(),
		OnView: func(_ string, client *attach.Client) {
			client.TokenRefresher = func(ctx context.Context) (string, error) {
				if stall.CompareAndSwap(true, false) {
					close(started)
					<-ctx.Done()
					return "", ctx.Err()
				}
				return h.AccessToken(), nil
			}
			active.Store(client)
		},
	})
	t.Cleanup(terminal.Cancel)
	terminal.Eventually(5*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if client := active.Load(); client == nil || !client.HasSnapshot() {
			return fmt.Errorf("terminal snapshot not ready: %s", screen.String())
		}
		return nil
	})
	stall.Store(true)
	terminal.Send("\x0ceTimed out rename\r")
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("management refresh did not start")
	}
	clk.Add(15 * time.Second)
	terminal.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("context deadline exceeded") {
			return fmt.Errorf("timeout not reported: %s", screen.String())
		}
		return nil
	})
	terminal.Send("\x0ceRecovered rename\r")
	terminal.Eventually(5*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		rows, err := fetchSessions(h.Endpoint(), h.AccessToken())
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.ID == "management-timeout" && row.Name == "Recovered rename" {
				return nil
			}
		}
		return fmt.Errorf("next rename remains unavailable: %+v %s", rows, screen.String())
	})
	terminal.Send("printf 'MANAGEMENT_%s\\n' $((2+3))\r")
	terminal.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if !screen.Contains("MANAGEMENT_5") {
			return fmt.Errorf("terminal input not rendered")
		}
		return nil
	})
}
