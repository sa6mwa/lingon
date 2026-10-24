package headless

import (
	"context"
	"encoding/json"
	"os"
	"strings"
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
	mode := os.Getenv("LINGON_TEST_CREDENTIAL_MODE")
	if mode == "unread" {
		_ = json.NewEncoder(pipe).Encode(StartupStatus{Status: "failed", Error: "fixture stopped before reading credentials"})
		os.Exit(0)
	}
	wantToken := launchTestToken(mode)
	for _, value := range append(os.Environ(), os.Args...) {
		if wantToken != "" && strings.Contains(value, wantToken) {
			os.Exit(3)
		}
	}
	token, err := ReadLaunchTokenFromEnv()
	if err != nil || token != wantToken {
		os.Exit(4)
	}
	if _, present := os.LookupEnv(LaunchTokenEnv); present {
		os.Exit(5)
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
	t.Setenv(LaunchTokenEnv, "invalid-parent-token-pipe")
	result, err := Launch(context.Background(), LaunchRequest{
		ConfigDir: dir, SessionID: id, Args: []string{"-test.run=^TestLaunchReadinessChild$"},
		Env: []string{"LINGON_LAUNCH_TEST_CHILD=1", "LINGON_TEST_SESSION=" + id, "LINGON_TEST_SOCKET=" + socket, "LINGON_SERVER_TLS_DIR=selected-trust"},
	})
	if err != nil || result.SessionID != id || result.SocketPath != socket || result.PID <= 0 {
		t.Fatalf("launch result=%+v error=%v", result, err)
	}
}

func launchTestToken(mode string) string {
	if mode == "small" {
		return "test-owned-private-credential"
	}
	if mode == "large" || mode == "unread" {
		return strings.Repeat("test-owned-private-credential", 8192)
	}
	return ""
}

func TestLaunchTransfersTokenThroughOwnedPipe(t *testing.T) {
	for _, mode := range []string{"small", "large", "unread"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			const id = "credential-child"
			socket, err := SocketPath(dir, id)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv(LaunchTokenEnv, "invalid-parent-token-pipe")
			result, err := Launch(context.Background(), LaunchRequest{
				ConfigDir: dir, SessionID: id, Args: []string{"-test.run=^TestLaunchReadinessChild$"},
				Token: launchTestToken(mode),
				Env: []string{"LINGON_LAUNCH_TEST_CHILD=1", "LINGON_TEST_SESSION=" + id, "LINGON_TEST_SOCKET=" + socket,
					"LINGON_SERVER_TLS_DIR=selected-trust", "LINGON_TEST_CREDENTIAL_MODE=" + mode, LaunchTokenEnv + "=invalid-override"},
			})
			if mode == "unread" {
				if err == nil || !strings.Contains(err.Error(), "fixture stopped before reading credentials") {
					t.Fatalf("expected startup failure without blocking on token write, got %v", err)
				}
				return
			}
			if err != nil || result.SessionID != id {
				t.Fatalf("credential launch: result=%+v error=%v", result, err)
			}
		})
	}
}

func TestReadLaunchTokenRejectsInheritedStdio(t *testing.T) {
	for _, fd := range []string{"", "invalid", "-1", "0", "1", "2", "99999999999999999999999999"} {
		t.Run(fd, func(t *testing.T) {
			t.Setenv(LaunchTokenEnv, fd)
			if _, err := ReadLaunchTokenFromEnv(); err == nil {
				t.Fatal("expected invalid descriptor to be rejected without accessing stdio")
			}
			if _, present := os.LookupEnv(LaunchTokenEnv); present {
				t.Fatal("invalid descriptor marker was retained")
			}
		})
	}
}
