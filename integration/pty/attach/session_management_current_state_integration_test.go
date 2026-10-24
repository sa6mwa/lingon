//go:build integration

package integrationptyattach_test

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/sessionmanage"
)

func TestSessionManagementCreationInheritsCurrentTargetState(t *testing.T) {
	for _, startupOffline := range []bool{true, false} {
		t.Run(fmt.Sprintf("startup_offline=%t", startupOffline), func(t *testing.T) {
			h := newHarness(t, ptytest.WithClock(clock.NewMock()))
			home := shortHomeDir(t)
			cfg := filepath.Join(home, lingon.DefaultConfigDirName)
			cleanupManagedTerminals(t, cfg)
			const source = "current-state-parent"
			args := []string{"-x", "--session", source, "--cols", "80", "--rows", "24", "--shell", fixedPromptEmitRowsBash(t),
				"--endpoint", h.Endpoint(), "--token", h.AccessToken(), "--insecure"}
			if startupOffline {
				args = append(args, "--offline")
			}
			cmd := exec.Command(buildLingonAttachBinary(t), args...)
			cmd.Env = testAttachCLIEnv(home)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("start parent: %v %s", err, output)
			}
			if startupOffline {
				local := startLingonAttachHeadlessCLI(t, home, source, 80, 24)
				t.Cleanup(local.Cancel)
				waitForPromptVisible(t, local, 8*time.Second)
				local.Send("\x0co")
				local.Eventually(8*time.Second, 50*time.Millisecond, func(ptytest.Screen) error {
					if !h.HasHost(source) {
						return fmt.Errorf("parent not online after explicit toggle")
					}
					return nil
				})
			}
			waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{source}, 8*time.Second)
			remote := startLingonAttachCLI(t, h, source, 120, 40)
			t.Cleanup(remote.Cancel)
			waitForPromptVisible(t, remote, 8*time.Second)
			if !startupOffline {
				remote.Send("\x0c0")
				remote.Send("stty size; echo PARENT_RESIZE_DONE\r")
				remote.Eventually(8*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
					if !screen.Contains("PARENT_RESIZE_DONE") || !screen.Contains("40 120") {
						return fmt.Errorf("parent resize not observed: %s", screen.String())
					}
					return nil
				})
			}
			var child string
			if startupOffline {
				remote.Send("\x0cc")
			} else {
				// Keep the existing attach on the parent: automatic selection of
				// the new headless child would resize it and mask stale creation.
				creator := &attach.Client{Endpoint: h.Endpoint(), AccessToken: h.AccessToken(), SessionID: source, Insecure: true}
				result, err := creator.Manage(context.Background(), sessionmanage.Request{SessionID: source, RequestID: "resized-parent-create", Kind: "create"})
				if err != nil {
					t.Fatal(err)
				}
				child = result.SessionID
			}
			remote.Eventually(8*time.Second, 50*time.Millisecond, func(ptytest.Screen) error {
				records, err := headless.NewStore(cfg).Reconcile()
				if err != nil {
					return err
				}
				for _, record := range records {
					if record.SessionID != source {
						child = record.SessionID
						if record.Offline {
							t.Fatalf("online target created an offline child: %+v", record)
						}
						if !h.HasHost(child) {
							return fmt.Errorf("child not discoverable")
						}
						return nil
					}
				}
				return fmt.Errorf("child not created")
			})
			if startupOffline {
				return
			}
			// Observe the child's actual snapshot without control or resize. The
			// resize was driven by real owned-PTY keypresses; creation used the
			// owner management API without selecting/resizing the new child.
			ctx, cancel := context.WithCancel(context.Background())
			observer := &attach.Client{Endpoint: h.Endpoint(), AccessToken: h.AccessToken(), SessionID: child, Insecure: true,
				Stdin: io.NopCloser(strings.NewReader("")), Stdout: io.Discard, Stderr: io.Discard,
				DisableResizePropagation: true, DisableSignalResize: true, DisableDesktopNotifications: true, Clock: h.Clock()}
			done := make(chan error, 1)
			go func() { done <- observer.RunDetached(ctx) }()
			t.Cleanup(func() { cancel(); <-done })
			remote.Eventually(8*time.Second, 50*time.Millisecond, func(ptytest.Screen) error {
				snap := observer.Snapshot()
				if snap == nil {
					return fmt.Errorf("child snapshot missing")
				}
				if snap.Cols != 120 || snap.Rows != 40 {
					return fmt.Errorf("child dimensions %dx%d; want resized target 120x40", snap.Cols, snap.Rows)
				}
				return nil
			})
		})
	}
}
