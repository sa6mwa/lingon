package headless

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// StartupStatusEnv identifies the child-owned startup readiness pipe.
const StartupStatusEnv = "__LINGON__HEADLESS_STARTUP_STATUS_FD"

// LaunchTokenEnv identifies a private inherited credential pipe, never a token.
const LaunchTokenEnv = "__LINGON__HEADLESS_LAUNCH_TOKEN_FD"

// LaunchRequest configures a detached copy of the current Lingon executable.
// Args are constructed by the host; remote clients never supply executable arguments.
type LaunchRequest struct {
	Executable string
	ConfigDir  string
	SessionID  string
	Args       []string
	// Env overrides host-selected child configuration. It is never supplied by remote clients.
	Env []string
	// Token is transferred through a private pipe, not process arguments or environment.
	Token string
}

// Launched identifies a ready, independently running terminal.
type Launched struct {
	SessionID  string
	SocketPath string
	PID        int
}

// Launch starts a detached child, waits for readiness and reaps it on exit.
// Cancellation after readiness does not terminate the child.
func Launch(ctx context.Context, req LaunchRequest) (Launched, error) {
	id, err := NormalizeSessionID(req.SessionID)
	if err != nil {
		return Launched{}, err
	}
	req.SessionID = id
	socket, err := SocketPath(req.ConfigDir, req.SessionID)
	if err != nil {
		return Launched{}, err
	}
	if err := os.MkdirAll(BaseDir(req.ConfigDir), 0700); err != nil {
		return Launched{}, err
	}
	exe := req.Executable
	if exe == "" {
		exe, err = os.Executable()
		if err != nil {
			return Launched{}, err
		}
	}
	r, w, err := os.Pipe()
	if err != nil {
		return Launched{}, err
	}
	defer r.Close()
	defer w.Close()
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return Launched{}, err
	}
	defer null.Close()
	child := exec.Command(exe, req.Args...)
	detach(child)
	child.Stdin, child.Stdout, child.Stderr = null, null, null
	child.ExtraFiles = []*os.File{w}
	var tokenWriter *os.File
	if req.Token != "" {
		tokenReader, writer, pipeErr := os.Pipe()
		if pipeErr != nil {
			return Launched{}, pipeErr
		}
		defer tokenReader.Close()
		defer writer.Close()
		tokenWriter = writer
		child.ExtraFiles = append(child.ExtraFiles, tokenReader)
	}
	for _, entries := range [][]string{os.Environ(), req.Env} {
		for _, entry := range entries {
			if !strings.HasPrefix(entry, EnvForeground+"=") && !strings.HasPrefix(entry, StartupStatusEnv+"=") && !strings.HasPrefix(entry, LaunchTokenEnv+"=") {
				child.Env = append(child.Env, entry)
			}
		}
	}
	child.Env = append(child.Env, EnvForeground+"="+ForegroundValue, StartupStatusEnv+"=3")
	if tokenWriter != nil {
		child.Env = append(child.Env, LaunchTokenEnv+"=4")
	}
	if err := child.Start(); err != nil {
		return Launched{}, err
	}
	_ = w.Close()
	go func() { _ = child.Wait() }()
	if tokenWriter != nil {
		// A child that fails before reading must not block cancellation or readiness.
		go func() {
			_, _ = io.WriteString(tokenWriter, req.Token)
			_ = tokenWriter.Close()
		}()
	}
	startupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	status, err := ReadStartupStatus(startupCtx, r)
	if err != nil {
		_ = child.Process.Kill()
		return Launched{}, fmt.Errorf("headless startup failed before reporting readiness: %w", err)
	}
	if status.Status != "ready" {
		_ = child.Process.Kill()
		return Launched{}, fmt.Errorf("headless startup failed: %s", status.Error)
	}
	if status.Session != req.SessionID || status.Socket != socket {
		_ = child.Process.Kill()
		return Launched{}, fmt.Errorf("headless startup identity mismatch")
	}
	return Launched{req.SessionID, socket, child.Process.Pid}, nil
}

// ReadLaunchTokenFromEnv consumes and closes the child-owned credential pipe.
// It clears the descriptor marker before any terminal subprocess can inherit it.
func ReadLaunchTokenFromEnv() (string, error) {
	raw, present := os.LookupEnv(LaunchTokenEnv)
	if !present {
		return "", nil
	}
	_ = os.Unsetenv(LaunchTokenEnv)
	fd, err := strconv.Atoi(raw)
	if err != nil || fd < 3 {
		return "", fmt.Errorf("invalid headless credential pipe")
	}
	pipe := os.NewFile(uintptr(fd), "headless-launch-token")
	defer pipe.Close()
	token, err := io.ReadAll(pipe)
	if err != nil {
		return "", fmt.Errorf("read headless credential pipe: %w", err)
	}
	if len(token) == 0 {
		return "", fmt.Errorf("empty headless launch credential")
	}
	return string(token), nil
}

// StartupStatus is the bounded readiness response written to a child-owned pipe.
type StartupStatus struct {
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
	Session string `json:"session,omitempty"`
	Socket  string `json:"socket,omitempty"`
}

// ReadStartupStatus decodes one readiness response, or returns on cancellation.
// The caller owns and closes the reader to interrupt outstanding pipe I/O.
func ReadStartupStatus(ctx context.Context, r io.Reader) (StartupStatus, error) {
	type response struct {
		status StartupStatus
		err    error
	}
	replies := make(chan response, 1)
	go func() {
		var status StartupStatus
		err := json.NewDecoder(io.LimitReader(r, 4096)).Decode(&status)
		replies <- response{status, err}
	}()
	select {
	case reply := <-replies:
		return reply.status, reply.err
	case <-ctx.Done():
		return StartupStatus{}, ctx.Err()
	}
}
