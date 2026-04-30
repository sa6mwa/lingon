//go:build integration
// +build integration

package integrationptysession_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
)

func TestCtrlLClearAutoHideTabBarWithBanner(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := ctrlLShell(t)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host1",
		SessionName: "host1",
		Shell:       shell,
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	bannerPresent := false
	host.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		if screen.Contains("connected to ") {
			bannerPresent = true
		}
		return nil
	})

	host.SendCtrlL()
	host.SendCtrlL()

	host.Eventually(2*time.Second, 50*time.Millisecond, func(_ ptytest.Screen) error {
		cur := host.Cursor()
		if cur.Row != 1 {
			return fmt.Errorf("expected cursor on row 1 after ctrl+l clear; got row %d col %d", cur.Row, cur.Col)
		}
		return nil
	})

	host.ExpectAfter(500*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if strings.Contains(row, "host1") {
			return fmt.Errorf("expected tab bar hidden after ctrl+l clear; got %q", row)
		}
		if bannerPresent {
			if !strings.Contains(row, "connected to ") {
				return fmt.Errorf("expected banner to remain on top row while tab bar auto-hides; got %q", row)
			}
			row2 := screen.Row(1)
			if strings.Contains(row2, "connected to ") {
				return fmt.Errorf("expected no duplicate banner on row 2; got %q", row2)
			}
		}
		return nil
	})
}

func TestCtrlLClearDisconnectBannerStaysBelowTopRow(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := ctrlLShell(t)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-disconnect-row1",
		SessionName: "host-disconnect-row1",
		Shell:       shell,
		Cols:        120,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	host.SendCtrlL()
	host.SendCtrlL()
	eventuallyWithClock(t, host.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		cur := host.Cursor()
		if cur.Row != 1 {
			return fmt.Errorf("expected cursor on row 1 after ctrl+l clear; got row %d col %d", cur.Row, cur.Col)
		}
		row := host.Screen().Row(0)
		if strings.Contains(row, "host-disconnect-row1") {
			return fmt.Errorf("expected tab bar hidden after ctrl+l clear; got %q", row)
		}
		if !strings.Contains(row, "PROMPT>") {
			return fmt.Errorf("expected prompt visible on row 1 before disconnect, got %q", row)
		}
		return nil
	})

	h.StopServer()

	bannerPresent := false
	host.Eventually(6*time.Second, 100*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if strings.Contains(row, "host-disconnect-row1") {
			return fmt.Errorf("expected tab bar hidden when cursor on top row; got %q", row)
		}
		if strings.Contains(row, "connection lost") || strings.Contains(row, "reconnecting") {
			bannerPresent = true
		}
		return nil
	})

	if !bannerPresent {
		return
	}

	deadline := ptytest.Now(h.Clock()).Add(3 * time.Second)
	for ptytest.Now(h.Clock()).Before(deadline) {
		screen := host.Screen()
		row1 := screen.Row(0)
		row2 := screen.Row(1)
		count := 0
		if strings.Contains(row1, "connection lost") || strings.Contains(row1, "reconnecting") {
			count++
		}
		if strings.Contains(row2, "connection lost") || strings.Contains(row2, "reconnecting") {
			count++
		}
		if count != 1 {
			t.Fatalf("expected a single disconnect banner on top rows; row1=%q row2=%q", row1, row2)
		}
		h.Advance(200 * time.Millisecond)
	}
}

func TestCtrlLClearDisconnectBannerPreservesPromptOnRowOne(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := ctrlLShell(t)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-disconnect-row1-prompt",
		SessionName: "host-disconnect-row1-prompt",
		Shell:       shell,
		Cols:        140,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	host.SendCtrlL()
	host.SendCtrlL()
	eventuallyWithClock(t, host.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		cur := host.Cursor()
		if cur.Row != 1 {
			return fmt.Errorf("expected cursor on row 1 after ctrl+l clear; got row %d col %d", cur.Row, cur.Col)
		}
		return nil
	})

	h.StopServer()

	eventuallyWithClock(t, h.Clock(), 6*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		hasReconnect := strings.Contains(row, "connection lost") || strings.Contains(row, "reconnecting")
		if !hasReconnect {
			return fmt.Errorf("expected reconnect banner on row 1, got %q", row)
		}
		if !strings.Contains(row, "PROMPT>") {
			return fmt.Errorf("expected prompt text preserved on row 1 while reconnect banner visible, got %q", row)
		}
		return nil
	})
}

func TestCtrlLToggleTabBarStaysHiddenWhileReconnectBannerVisible(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := ctrlLShell(t)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-tab-toggle-banner",
		SessionName: "host-tab-toggle-banner",
		Shell:       shell,
		Cols:        140,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)
	host.SendCtrlL()
	host.Send("c")
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 2, 6*time.Second)

	host.Send("echo READY\n")
	if !screenContainsWithin(host, "READY", 2*time.Second) {
		t.Fatalf("expected READY output before disconnect")
	}

	eventuallyWithClock(t, h.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		if cur := host.Cursor(); cur.Row <= 1 {
			return fmt.Errorf("expected cursor below row 1 before reconnect, got row=%d col=%d", cur.Row, cur.Col)
		}
		row := host.Screen().Row(0)
		if !strings.Contains(row, "host-tab-toggle-banner") {
			return fmt.Errorf("expected tab bar visible before reconnect, got row=%q", row)
		}
		return nil
	})

	h.StopServer()
	host.Send("echo DISCONNECT_TRIGGER\n")

	eventuallyWithClock(t, h.Clock(), 6*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		if !strings.Contains(row, "connection lost") && !strings.Contains(row, "reconnecting") {
			return fmt.Errorf("expected reconnect banner on row 1, got %q", row)
		}
		return nil
	})

	host.SendCtrlL()
	host.Send("b")

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		if strings.Contains(row, "host-tab-toggle-banner") {
			return fmt.Errorf("expected tab bar hidden after ctrl+l b, got %q", row)
		}
		return nil
	})

	host.Send("echo TYPE_CHECK\n")
	if !screenContainsWithin(host, "TYPE_CHECK", 2*time.Second) {
		t.Fatalf("expected TYPE_CHECK output after typing")
	}

	eventuallyWithClock(t, h.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		hasBanner := strings.Contains(row, "connection lost") || strings.Contains(row, "reconnecting")
		if !hasBanner {
			return fmt.Errorf("expected reconnect banner to remain visible, got %q", row)
		}
		if strings.Contains(row, "host-tab-toggle-banner") {
			return fmt.Errorf("expected tab bar to remain hidden after typing while reconnect banner visible, got %q", row)
		}
		return nil
	})
}

func TestReconnectBannerPreservesLeftContentOnTopRowDuringLocalOutput(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := ctrlLShell(t)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-disconnect-live-output",
		SessionName: "host-disconnect-live-output",
		Shell:       shell,
		Cols:        140,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	host.Send("echo READY\n")
	if !screenContainsWithin(host, "READY", 2*time.Second) {
		t.Fatalf("expected READY output before disconnect")
	}

	h.StopServer()
	// Force a local write after relay shutdown so publisher reconnect state
	// transitions deterministically before row-1 banner assertions.
	host.Send("echo DISCONNECT_TRIGGER\n")

	eventuallyWithClock(t, h.Clock(), 6*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		if !strings.Contains(row, "connection lost") && !strings.Contains(row, "reconnecting") {
			return fmt.Errorf("expected reconnect banner on row 1, got %q", row)
		}
		return nil
	})

	host.Send("ls -la\n")

	eventuallyWithClock(t, h.Clock(), 6*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		if !strings.Contains(row, "connection lost") && !strings.Contains(row, "reconnecting") {
			return fmt.Errorf("expected reconnect banner on row 1, got %q", row)
		}
		hasLeftContent := strings.Contains(row, "PROMPT>") || strings.Contains(row, "host-disconnect-live-output")
		if !hasLeftContent {
			return fmt.Errorf("expected left-side row1 framebuffer content preserved with reconnect banner badge, got %q", row)
		}
		return nil
	})
}

func TestReconnectBannerPreservesLeftContentOnTopRowDuringRapidOutput(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := ctrlLShell(t)

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "host-disconnect-rapid-output",
		SessionName: "host-disconnect-rapid-output",
		Shell:       shell,
		Cols:        140,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	host.Send("echo READY\n")
	if !screenContainsWithin(host, "READY", 2*time.Second) {
		t.Fatalf("expected READY output before disconnect")
	}

	h.StopServer()
	// Force a local write after relay shutdown so publisher reconnect state
	// transitions deterministically before row-1 banner assertions.
	host.Send("echo DISCONNECT_TRIGGER\n")

	eventuallyWithClock(t, h.Clock(), 6*time.Second, 50*time.Millisecond, func() error {
		row := host.Screen().Row(0)
		if !strings.Contains(row, "connection lost") && !strings.Contains(row, "reconnecting") {
			return fmt.Errorf("expected reconnect banner on row 1, got %q", row)
		}
		return nil
	})

	for i := 0; i < 24; i++ {
		token := fmt.Sprintf("BANNER_STRESS_%02d", i)
		host.Send("echo " + token + "\n")
		eventuallyWithClock(t, h.Clock(), 3*time.Second, 25*time.Millisecond, func() error {
			row := host.Screen().Row(0)
			hasBanner := strings.Contains(row, "connection lost") || strings.Contains(row, "reconnecting")
			if !hasBanner {
				return nil
			}
			hasLeftContent := strings.Contains(row, "PROMPT>") || strings.Contains(row, "host-disconnect-rapid-output")
			if !hasLeftContent {
				return fmt.Errorf("expected left-side row1 framebuffer content preserved with reconnect banner badge, got %q", row)
			}
			return nil
		})
	}
}

func ctrlLShell(t *testing.T) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "ctrl-l-shell.sh")
	const script = `#!/bin/sh
if [ -x /bin/bash ]; then
  exec /bin/bash --noprofile --norc -i
fi
exec /bin/sh -i
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write shell wrapper: %v", err)
	}
	return scriptPath
}
