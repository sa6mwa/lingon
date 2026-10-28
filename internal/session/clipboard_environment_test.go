package session_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/relayhost"
)

// Invoked only by a child shell with stdio attached to ptytest-owned PTYs.
func TestClipboardEnvironmentPTYHelper(t *testing.T) {
	if os.Getenv("LINGON_CLIPBOARD_ENV_CHILD") != "1" {
		return
	}
	fmt.Print("\r\nENV_READY\r\n")
	var key [1]byte
	if _, err := io.ReadFull(os.Stdin, key[:]); err != nil {
		os.Exit(1)
	}
	if authority, present := os.LookupEnv("XAUTHORITY"); present {
		fmt.Printf("\r\nAUTH_VALUE:%s\r\n", authority)
	} else {
		fmt.Print("\r\nAUTH_UNSET\r\n")
	}
	fmt.Printf("DISPLAY_VALUE:%s\r\n", os.Getenv("DISPLAY"))
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

func TestClipboardInheritedAuthorityPTY(t *testing.T) {
	for _, backend := range []string{"session", "sdk"} {
		for _, authority := range []string{"unset", "empty", "explicit"} {
			t.Run(backend+"/"+authority, func(t *testing.T) {
				// A synthetic inherited display cannot access the desktop; these
				// regressions observe child env after an actual terminal keypress.
				t.Setenv("DISPLAY", "127.0.0.1:65432")
				t.Setenv("XAUTHORITY", "")
				want := "AUTH_VALUE:"
				switch authority {
				case "unset":
					if err := os.Unsetenv("XAUTHORITY"); err != nil {
						t.Fatal(err)
					}
					want = "AUTH_UNSET"
				case "explicit":
					path := filepath.Join(t.TempDir(), "cookie")
					t.Setenv("XAUTHORITY", path)
					want += path
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				exe, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				shell := filepath.Join(t.TempDir(), "environment-shell")
				script := "#!/bin/sh\nstty -icanon -echo\nexport LINGON_CLIPBOARD_ENV_CHILD=1 LINGON_TEST_OWN_PTY=1\nexec '" + strings.ReplaceAll(exe, "'", "'\\''") + "' -test.run='^TestClipboardEnvironmentPTYHelper$'\n"
				if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
				recorder := ptytest.NewWSRecorder()
				h := ptytest.New(t, ptytest.WithWSRecorder(recorder))
				defer h.Close()
				var terminal *ptytest.PTYSession
				if backend == "session" {
					terminal = h.StartHost(ptytest.HostOptions{SessionID: "env", Shell: shell, Cols: 180, Rows: 20, DisablePublish: true})
				} else {
					host := &relayhost.Host{Endpoint: h.Endpoint(), Token: h.AccessToken(), TLSDir: h.TLSDir(), SessionID: "env", Command: []string{shell}, Cols: 180, Rows: 20}
					done := make(chan error, 1)
					go func() { done <- host.Run(ctx) }()
					defer func() {
						cancel()
						if err := <-done; err != nil {
							t.Error(err)
						}
					}()
					waitClipboardRegistration(t, ctx, recorder, "env")
					terminal = h.StartAttach(ptytest.AttachOptions{SessionID: "env", Cols: 180, Rows: 20})
				}
				defer terminal.Close()
				waitClipboardScreen(t, ctx, terminal, "ENV_READY")
				terminal.Send("e")
				// Wait for the final marker before checking authority, so a missing
				// variable fails immediately rather than relying on a timeout.
				waitClipboardScreen(t, ctx, terminal, "DISPLAY_VALUE:127.0.0.1:65432")
				found := false
				for _, row := range strings.Split(terminal.Screen().String(), "\n") {
					found = found || strings.TrimSpace(row) == want
				}
				if !found {
					t.Fatalf("inherited authority changed; want %q:\n%s", want, terminal.Screen().String())
				}
			})
		}
	}
}
