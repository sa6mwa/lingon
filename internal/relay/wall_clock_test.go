package relay

import (
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
)

func TestWallClockControlsInactivityRearmAndEventTimestamps(t *testing.T) {
	clk := clock.NewMock()
	store := NewStore()
	hub := NewHub(nil)
	store.CreateSession(Session{ID: "clocked", Username: "alice", Name: "clocked", Status: "active", CreatedAt: clk.Now()})
	peer := &fakeConn{id: "clocked-host", role: RoleHost, sessionID: "clocked", scope: ShareScopeControl}
	if err := hub.RegisterHost(peer, "clocked", 80, 24); err != nil {
		t.Fatal(err)
	}
	server := NewHTTPServer(store, nil, nil, nil, hub)
	server.WallClock = clk
	server.ConfigureWall(time.Second, []time.Duration{time.Second})
	svc := server.wallService()
	t.Cleanup(svc.close)
	if enabled, _ := svc.setInactivity("alice", "clocked", "alice", true, svc.now()); !enabled {
		t.Fatal("inactivity did not enable")
	}
	clk.Add(999 * time.Millisecond)
	if peer.sentLen() != 0 {
		t.Fatal("inactivity fired before the injected deadline")
	}
	clk.Add(time.Millisecond)
	if peer.sentLen() != 1 {
		t.Fatalf("injected deadline did not fire: %d", peer.sentLen())
	}
	events, _, _ := svc.listEvents("alice", 0, 10, svc.now())
	if len(events) != 1 || !events[0].CreatedAt.Equal(clk.Now()) {
		t.Fatalf("event timestamp did not use injected time: %+v", events)
	}
	clk.Add(5 * time.Second)
	if peer.sentLen() != 1 {
		t.Fatal("idle wall repeated without activity")
	}
	svc.markActivity("clocked", svc.now())
	clk.Add(999 * time.Millisecond)
	if peer.sentLen() != 1 {
		t.Fatal("rearmed inactivity fired early")
	}
	clk.Add(time.Millisecond)
	if peer.sentLen() != 2 {
		t.Fatalf("terminal activity did not rearm inactivity: %d", peer.sentLen())
	}
	svc.setInactivity("alice", "clocked", "alice", false, svc.now())
	svc.markActivity("clocked", svc.now())
	clk.Add(5 * time.Second)
	if peer.sentLen() != 2 {
		t.Fatal("disabled inactivity was rearmed")
	}
}
