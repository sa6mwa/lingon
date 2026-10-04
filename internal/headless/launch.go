package headless

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// StartupStatusEnv identifies the child-owned startup readiness pipe.
const StartupStatusEnv = "__LINGON__HEADLESS_STARTUP_STATUS_FD"

// LaunchRequest configures a detached copy of the current Lingon executable.
// Args are constructed by the host; remote clients never supply executable arguments.
type LaunchRequest struct {
	Executable string
	ConfigDir  string
	SessionID  string
	Args       []string
	// Env overrides host-selected child configuration. It is never supplied by remote clients.
	Env []string
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
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, EnvForeground+"=") && !strings.HasPrefix(entry, StartupStatusEnv+"=") {
			child.Env = append(child.Env, entry)
		}
	}
	child.Env = append(child.Env, req.Env...)
	child.Env = append(child.Env, EnvForeground+"="+ForegroundValue, StartupStatusEnv+"=3")
	if err := child.Start(); err != nil {
		return Launched{}, err
	}
	_ = w.Close()
	go func() { _ = child.Wait() }()
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
