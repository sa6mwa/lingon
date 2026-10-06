package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/sessionmanage"
)

// This helper captures the real detached-launch arguments using only the owned
// readiness pipe. It starts no terminal, PTY, shell or interactive program.
func managementGeometryChild() bool {
	if os.Getenv("LINGON_TEST_MANAGEMENT_GEOMETRY_CHILD") != "1" || os.Getenv(headless.StartupStatusEnv) != "3" {
		return false
	}
	var cols, rows int
	offline := false
	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--offline":
			offline = true
		case "--cols":
			cols, _ = strconv.Atoi(os.Args[i+1])
		case "--rows":
			rows, _ = strconv.Atoi(os.Args[i+1])
		}
	}
	pipe := os.NewFile(3, "owned-management-readiness")
	_ = json.NewEncoder(pipe).Encode(headless.StartupStatus{Status: "failed", Error: fmt.Sprintf("captured-offline=%t;captured-geometry=%dx%d", offline, cols, rows)})
	_ = pipe.Close()
	return true
}

func TestDetachedCreationCapturesCoherentGeometryDuringResize(t *testing.T) {
	t.Setenv("LINGON_TEST_MANAGEMENT_GEOMETRY_CHILD", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r := New(Options{ConfigDir: t.TempDir(), Cols: 80, Rows: 24, HeadlessExecutable: executable})
	// Inert target state only: no newLocalSession, Run, terminal or raw mode.
	local := &localSession{id: "parent", cols: 80, rows: 24}
	r.localSessions = map[string]*localSession{"parent": local}
	started, stop, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(joined)
		close(started)
		for {
			select {
			case <-stop:
				return
			default:
				local.emuMu.Lock()
				local.cols, local.rows = 120, 40
				local.emuMu.Unlock()
				local.emuMu.Lock()
				local.cols, local.rows = 80, 24
				local.emuMu.Unlock()
			}
		}
	}()
	<-started
	defer func() { close(stop); <-joined }()
	for i := 0; i < 20; i++ {
		result := r.Manage(context.Background(), sessionmanage.Request{RequestID: fmt.Sprintf("geometry-%d", i), SessionID: "parent", Kind: "create"})
		if !strings.HasSuffix(result.Error, "captured-geometry=80x24") && !strings.HasSuffix(result.Error, "captured-geometry=120x40") {
			t.Errorf("child received an incoherent geometry pair or failed its fixture: %+v", result)
		}
	}
}

func TestDetachedCreationUsesCurrentTargetState(t *testing.T) {
	t.Setenv("LINGON_TEST_MANAGEMENT_GEOMETRY_CHILD", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, startupOffline := range []bool{false, true} {
		t.Run(fmt.Sprintf("startup_offline=%t", startupOffline), func(t *testing.T) {
			r := New(Options{ConfigDir: t.TempDir(), Cols: 80, Rows: 24,
				Offline: startupOffline, Publish: true, Endpoint: "https://relay.invalid/v1", HeadlessExecutable: executable})
			// Inert synchronized state: no PTY, raw mode or terminal access.
			local := &localSession{id: "parent", cols: 120, rows: 40, offline: startupOffline}
			local.ToggleOffline()
			r.localSessions = map[string]*localSession{"parent": local}
			result := r.Manage(context.Background(), sessionmanage.Request{RequestID: "current-state", SessionID: "parent", Kind: "create"})
			want := fmt.Sprintf("captured-offline=%t;captured-geometry=120x40", !startupOffline)
			if !strings.HasSuffix(result.Error, want) {
				t.Fatalf("child state: %+v; want %s", result, want)
			}
		})
	}
}
