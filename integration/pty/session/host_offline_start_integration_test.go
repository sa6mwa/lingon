//go:build integration
// +build integration

package integrationptysession_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pkt.systems/lingon/internal/authstore"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/session"
)

func TestHostStartsWithValidRefreshTokenWhenRelayDown(t *testing.T) {
	t.Setenv("PS1", "PROMPT> ")
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}

	clk := clock.New()
	authPath := filepath.Join(t.TempDir(), "auth.json")
	state := authstore.State{
		Endpoint:         "https://127.0.0.1:1/v1",
		RefreshToken:     "refresh-token",
		RefreshExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if err := authstore.Save(authPath, state); err != nil {
		t.Fatalf("save auth: %v", err)
	}

	master, slave := ptytest.OpenPTY(t, 80, 24)
	sess := ptytest.NewPTYSessionWithClock(t, master, slave, 80, 24, clk)
	runner := session.New(session.Options{
		Endpoint:  state.Endpoint,
		Token:     "",
		AuthFile:  authPath,
		SessionID: "offline-host",
		Shell:     shell,
		Cols:      80,
		Rows:      24,
		Publish:   true,
		Stdin:     slave,
		Stdout:    slave,
		Clock:     clk,
	})

	go func() {
		sess.SetRunErr(runner.Run(sess.Context()))
	}()

	advanceTestClock(sess.Clock(), 200*time.Millisecond)
	sess.Send("echo READY\n")
	eventuallyWithClock(t, sess.Clock(), 2*time.Second, 50*time.Millisecond, func() error {
		if !sess.Screen().Contains("READY") {
			return fmt.Errorf("expected READY output while relay is down")
		}
		return nil
	})

	sess.Cancel()
	if exited, err := sess.WaitErr(2 * time.Second); exited && err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected run error: %v", err)
	}
}
