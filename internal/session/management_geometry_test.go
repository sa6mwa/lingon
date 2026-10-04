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
	for i := 1; i+1 < len(os.Args); i++ {
		switch os.Args[i] {
		case "--cols":
			cols, _ = strconv.Atoi(os.Args[i+1])
		case "--rows":
			rows, _ = strconv.Atoi(os.Args[i+1])
		}
	}
	pipe := os.NewFile(3, "owned-management-readiness")
	_ = json.NewEncoder(pipe).Encode(headless.StartupStatus{Status: "failed", Error: fmt.Sprintf("captured-geometry=%dx%d", cols, rows)})
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
	// Inert session identity only: no newLocalSession, Run, terminal or raw mode.
	r.localSessions = map[string]*localSession{"parent": {id: "parent"}}
	started, stop, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(joined)
		r.ResizeActive(80, 24)
		close(started)
		for {
			select {
			case <-stop:
				return
			default:
				r.ResizeActive(120, 40)
				r.ResizeActive(80, 24)
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
