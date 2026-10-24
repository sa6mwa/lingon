//go:build integration

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

func TestHostAndAttachBottomAnchoredFeedOutsideScrollRegion(t *testing.T) {
	// The script and all terminal controls run inside StartHost's owned child PTY.
	script := filepath.Join(t.TempDir(), "feed.sh")
	const source = `#!/bin/sh
stty -echo
printf '\033[?6l\033[1;6r\033[7;1HSTATUS\033[8;1HREADY>'
while IFS= read -r command; do
    case "$command" in
        1) up=6; text=one ;;
        2) up=5; text=two ;;
        3) up=4; text=three ;;
        *) continue ;;
    esac
    printf '\033[1;6r\033[65535;1H\033[%sA%s\r\n\033[0m\033[65535;1H' "$up" "$text"
done
`
	if err := os.WriteFile(script, []byte(source), 0o700); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{SessionID: "cup-feed", Shell: script, Cols: 40, Rows: 8})
	t.Cleanup(host.Cancel)
	ready := func(screen ptytest.Screen) error {
		if !screen.Contains("READY>") {
			return fmt.Errorf("waiting for the feed script")
		}
		return nil
	}
	host.Eventually(3*time.Second, 10*time.Millisecond, ready)
	waitForSessionCountSession(t, h.Clock(), h.Endpoint(), h.AccessToken(), h.AuthFile(), 1, 5*time.Second)
	client := h.StartAttach(ptytest.AttachOptions{
		SessionID: "cup-feed", Cols: 40, Rows: 8, RequestControl: true, RawInput: true,
	})
	t.Cleanup(client.Cancel)
	client.Eventually(3*time.Second, 10*time.Millisecond, ready)
	lines := []string{"one", "two", "three"}
	for i := range lines {
		client.Send(fmt.Sprintf("%d\n", i+1))
		check := func(screen ptytest.Screen) error {
			for row, want := range lines[:i+1] {
				if got := strings.TrimSpace(screen.Row(row + 1)); got != want {
					return fmt.Errorf("feed row %d = %q, want %q", row+2, got, want)
				}
			}
			for row, want := range map[int]string{6: "STATUS", 7: "READY>"} {
				if got := strings.TrimSpace(screen.Row(row)); got != want {
					return fmt.Errorf("reserved row %d = %q, want %q", row+1, got, want)
				}
			}
			return nil
		}
		host.Eventually(3*time.Second, 10*time.Millisecond, check)
		client.Eventually(3*time.Second, 10*time.Millisecond, check)
	}
}
