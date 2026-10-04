//go:build integration && linux

package integrationptyattach_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/sessionmanage"
)

func TestSessionManagementChildCredentialsStayOutOfProcessMetadata(t *testing.T) {
	for _, useAuthFile := range []bool{true, false} {
		t.Run(fmt.Sprintf("auth_file=%t", useAuthFile), func(t *testing.T) {
			h := newHarness(t, ptytest.WithClock(clock.NewMock()))
			home := shortHomeDir(t)
			cfg := filepath.Join(home, lingon.DefaultConfigDirName)
			cleanupManagedTerminals(t, cfg)
			authFile := ""
			if useAuthFile {
				authFile = h.AuthFile()
			}
			host := startManagementHostCLIWithAuthFile(t, h, home, authFile)
			waitForPromptVisible(t, host, 8*time.Second)
			waitForSessionsWithTimeout(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"management-host"}, 8*time.Second)
			// Drive the real attach shortcut; the host chooses its child's credentials.
			remote := startLingonAttachCLI(t, h, "management-host", 100, 24)
			t.Cleanup(remote.Cancel)
			waitForPromptVisible(t, remote, 8*time.Second)
			remote.Send("\x0cc")
			var child headless.SessionRecord
			remote.Eventually(8*time.Second, 50*time.Millisecond, func(screen ptytest.Screen) error {
				records, err := headless.NewStore(cfg).Reconcile()
				if err != nil {
					return err
				}
				if len(records) != 1 || !h.HasHost(records[0].SessionID) {
					return fmt.Errorf("waiting for the authenticated child")
				}
				child = records[0]
				return nil
			})
			for _, metadata := range []string{"cmdline", "environ"} {
				data, err := os.ReadFile(fmt.Sprintf("/proc/%d/%s", child.PID, metadata))
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(data, []byte(h.AccessToken())) || bytes.Contains(data, []byte("--token")) {
					t.Errorf("child leaks a relay credential in %s", metadata)
				}
				if metadata == "cmdline" && useAuthFile && !bytes.Contains(data, []byte(authFile)) {
					t.Error("child did not reuse the host auth file")
				}
			}
			// Creation is not just ready: its own authenticated connection can rename.
			client := &attach.Client{Endpoint: h.Endpoint(), AccessToken: h.AccessToken(), SessionID: child.SessionID, Insecure: true}
			result, err := client.Manage(context.Background(), sessionmanage.Request{
				SessionID: child.SessionID, RequestID: "credential-child-rename", Kind: "rename", Name: "Credential child",
			})
			if err != nil || result.Error != "" {
				t.Fatalf("child management failed: %v %s", err, result.Error)
			}
			if _, err := waitForSessionName(t, h.Clock(), h.Endpoint(), h.AccessToken(), "Credential child", 8*time.Second); err != nil {
				t.Fatal(err)
			}
		})
	}
}
