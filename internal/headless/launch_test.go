package headless

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

// This child exercises only the readiness pipe; it opens no terminal or PTY.
func TestLaunchReadinessChild(t *testing.T) {
	if os.Getenv("LINGON_LAUNCH_TEST_CHILD") != "1" {
		return
	}
	pipe := os.NewFile(3, "owned-readiness")
	if os.Getenv(StartupStatusEnv) != "3" || os.Getenv(EnvForeground) != ForegroundValue || os.Getenv("LINGON_SERVER_TLS_DIR") != "selected-trust" {
		os.Exit(2)
	}
	_ = json.NewEncoder(pipe).Encode(StartupStatus{Status: "ready", Session: os.Getenv("LINGON_TEST_SESSION"), Socket: os.Getenv("LINGON_TEST_SOCKET")})
	_ = pipe.Close()
	os.Exit(0)
}

func TestLaunchUsesHostEnvironmentAndOwnedReadiness(t *testing.T) {
	dir := t.TempDir()
	const id = "environment-child"
	socket, err := SocketPath(dir, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LINGON_SERVER_TLS_DIR", "inherited-trust")
	t.Setenv(StartupStatusEnv, "invalid-parent-pipe")
	result, err := Launch(context.Background(), LaunchRequest{
		ConfigDir: dir, SessionID: id, Args: []string{"-test.run=^TestLaunchReadinessChild$"},
		Env: []string{"LINGON_LAUNCH_TEST_CHILD=1", "LINGON_TEST_SESSION=" + id, "LINGON_TEST_SOCKET=" + socket, "LINGON_SERVER_TLS_DIR=selected-trust"},
	})
	if err != nil || result.SessionID != id || result.SocketPath != socket || result.PID <= 0 {
		t.Fatalf("launch result=%+v error=%v", result, err)
	}
}
