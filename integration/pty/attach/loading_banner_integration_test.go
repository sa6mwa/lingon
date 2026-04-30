//go:build integration
// +build integration

package integrationptyattach_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/testutil"
)

func TestAttachFastReadyDoesNotLeaveLoadingBanner(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "loading-fast",
		SessionName: "loading-fast",
		Shell:       "/bin/sh",
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"loading-fast"})

	var activeMu sync.Mutex
	activeID := ""
	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "loading-fast",
		Cols:      100,
		Rows:      30,
		OnActive: func(id string) {
			activeMu.Lock()
			activeID = id
			activeMu.Unlock()
		},
		OnView: func(id string, client *attach.Client) {
			viewsMu.Lock()
			views[id] = client
			viewsMu.Unlock()
		},
	})
	t.Cleanup(attachSess.Cancel)

	waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)

	ptytest.Advance(h.Clock(), 4*time.Second)
	attachSess.Eventually(2*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if strings.Contains(row, "connected to ") {
			return fmt.Errorf("expected connected banner cleared, row=%q", row)
		}
		if strings.Contains(row, "loading from relay") {
			return fmt.Errorf("expected loading banner cleared after ready, row=%q", row)
		}
		return nil
	})
}

func TestMultiAttachStartupConnectedBannerOverlaysPromptInsteadOfBlankingRow(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "loading-connected-banner-overlay",
		SessionName: "loading-connected-banner-overlay",
		Shell:       writeAttachPromptShell(t),
		Cols:        100,
		Rows:        30,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"loading-connected-banner-overlay"})
	if !screenContainsWithin(host, "PROMPT>", 3*time.Second) {
		t.Fatalf("expected host prompt before attach startup")
	}

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "loading-connected-banner-overlay",
		Cols:      100,
		Rows:      30,
	})
	t.Cleanup(attachSess.Cancel)

	attachSess.Eventually(2*time.Second, 40*time.Millisecond, func(screen ptytest.Screen) error {
		row := screen.Row(0)
		if !strings.Contains(row, "connected to ") {
			return fmt.Errorf("waiting for connected banner, row=%q", row)
		}
		if !strings.Contains(row, "PROMPT>") {
			return fmt.Errorf("expected prompt preserved under connected banner overlay, row=%q\nscreen:\n%s", row, screen.String())
		}
		return nil
	})
}

func writeAttachPromptShell(t *testing.T) string {
	t.Helper()
	path := filepath.Join(testutil.TempDir(t), "attach-startup-prompt.sh")
	const script = "#!/usr/bin/env bash\nexport PS1='PROMPT> '\nexec /bin/bash --noprofile --norc -i\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write attach startup prompt shell: %v", err)
	}
	return path
}
