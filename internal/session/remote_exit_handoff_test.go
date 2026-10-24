package session

import (
	"bytes"
	"testing"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/clock"
)

func TestUpdatingTabsPreservesSessionUntilLifecycleHandoff(t *testing.T) {
	r := New(Options{Clock: clock.NewMock()})
	// Tab-list maintenance must not silently activate a fallback. Activation
	// must hide the old view and invalidate its incompatible render baseline.
	r.localOrder = []string{"local"}
	r.setActiveSession("remote", false)
	r.updateTabs([]remoteSessionInfo{{ID: "local", Name: "local"}})
	if id, local := r.activeSession(); id != "remote" || local {
		t.Fatalf("tab update bypassed the lifecycle handoff: active=%q local=%v", id, local)
	}
}

func TestExplicitRemoteCloseDetachesRenderingBeforeHandoff(t *testing.T) {
	for _, phase := range []string{"sessions callback", "close callback", "after close"} {
		t.Run(phase, func(t *testing.T) {
			var output bytes.Buffer
			client := &attach.Client{
				SessionID: "remote", Stdout: &output, Clock: clock.NewMock(),
				TermSize: func() (int, int) { return 40, 10 },
			}
			client.RenderDisabled()
			if output.Len() == 0 {
				t.Fatal("fixture must render before closing")
			}
			output.Reset()
			m := newRemoteManager(remoteOptions{Clock: clock.NewMock()})
			m.sessions = []remoteSessionInfo{{ID: "remote", Name: "remote"}}
			m.views["remote"] = &remoteView{id: "remote", client: client, visible: true}
			if phase == "sessions callback" {
				m.onSessions = func([]remoteSessionInfo) { client.RenderDisabled() }
			}
			if phase == "close callback" {
				m.onViewClosed = func(string, error) { client.RenderDisabled() }
			}
			m.handleExplicitSessionClosed("remote")
			if phase == "after close" {
				client.RenderDisabled()
			}
			if output.Len() != 0 {
				t.Fatalf("closed renderer wrote %d bytes during %s", output.Len(), phase)
			}
			if m.HasSession("remote") {
				t.Fatal("explicitly closed session remains available")
			}
		})
	}
}
