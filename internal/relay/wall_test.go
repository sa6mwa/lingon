package relay

import (
	"testing"
	"time"

	"pkt.systems/lingon/internal/protocolpb"
)

func TestSanitizeWallMessage(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "strips ansi csi",
			in:   "hello \x1b[31mred\x1b[0m world",
			want: "hello red world",
		},
		{
			name: "strips ansi osc",
			in:   "a\x1b]0;title\x07b\x1b]8;;https://x\x1b\\link\x1b]8;;\x1b\\c",
			want: "ablinkc",
		},
		{
			name: "normalizes whitespace and controls",
			in:   "one\t\t two\nthree\r\nfour\x00\x07",
			want: "one two three four",
		},
		{
			name: "keeps emoji letters digits punctuation",
			in:   "Ping 🚀 café 123?!",
			want: "Ping 🚀 café 123?!",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeWallMessage(tt.in); got != tt.want {
				t.Fatalf("sanitizeWallMessage(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestWallServiceSendUserWallScopesToParticipantSessions(t *testing.T) {
	store := NewStore()
	hub := NewHub(nil)
	now := time.Now().UTC()
	store.CreateSession(Session{ID: "s1", Username: "alice", Status: "active", CreatedAt: now, LastActiveAt: now})
	store.CreateSession(Session{ID: "s2", Username: "alice", Status: "active", CreatedAt: now, LastActiveAt: now})

	host := &fakeConn{id: "host-s1", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
	client := &fakeConn{id: "client-s1", role: RoleClient, sessionID: "s1", scope: ShareScopeControl}
	if err := hub.RegisterHost(host, "s1", 80, 24); err != nil {
		t.Fatalf("RegisterHost: %v", err)
	}
	hub.RegisterClient(client, "s1", "client-s1", true)

	svc := newWallService(store, hub, nil, 5*time.Second, []time.Duration{5 * time.Minute})
	sent, err := svc.sendUserWall("alice", "alice@127.0.0.1", "hello", now)
	if err != nil {
		t.Fatalf("sendUserWall: %v", err)
	}
	if sent != 1 {
		t.Fatalf("sent = %d, want 1", sent)
	}
	if len(host.sent) != 1 {
		t.Fatalf("host frames = %d, want 1", len(host.sent))
	}
	if len(client.sent) != 1 {
		t.Fatalf("client frames = %d, want 1", len(client.sent))
	}
	if got := host.sent[0].GetWall(); got == nil || got.Message != "hello" {
		t.Fatalf("unexpected host wall frame: %#v", host.sent[0].GetWall())
	}
	if got := host.sent[0].GetWall(); got == nil || got.GetId() == 0 {
		t.Fatalf("expected wall frame to carry event id, got %#v", host.sent[0].GetWall())
	}
}

func TestWallServiceSendUserWallSanitizesMessage(t *testing.T) {
	store := NewStore()
	hub := NewHub(nil)
	now := time.Now().UTC()
	store.CreateSession(Session{ID: "s1", Username: "alice", Status: "active", CreatedAt: now, LastActiveAt: now})
	host := &fakeConn{id: "host-s1", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
	if err := hub.RegisterHost(host, "s1", 80, 24); err != nil {
		t.Fatalf("RegisterHost: %v", err)
	}
	svc := newWallService(store, hub, nil, 5*time.Second, []time.Duration{5 * time.Minute})
	sent, err := svc.sendUserWall("alice", "alice@127.0.0.1", "hi\x1b[31m!\x1b[0m\n\tok\x00", now)
	if err != nil {
		t.Fatalf("sendUserWall: %v", err)
	}
	if sent != 1 {
		t.Fatalf("sent = %d, want 1", sent)
	}
	if len(host.sent) != 1 {
		t.Fatalf("host frames = %d, want 1", len(host.sent))
	}
	wall := host.sent[0].GetWall()
	if wall == nil {
		t.Fatalf("missing wall payload")
	}
	if wall.Message != "hi! ok" {
		t.Fatalf("sanitized message = %q, want %q", wall.Message, "hi! ok")
	}
}

func TestWallServiceInactivityFiresAfterEachActivityWhileEnabled(t *testing.T) {
	store := NewStore()
	hub := NewHub(nil)
	now := time.Now().UTC()
	store.CreateSession(Session{ID: "s1", Username: "alice", Name: "session-a", Status: "active", CreatedAt: now, LastActiveAt: now})
	store.CreateSession(Session{ID: "s2", Username: "alice", Name: "session-b", Status: "active", CreatedAt: now, LastActiveAt: now})
	host := &fakeConn{id: "host-s1", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
	peer := &fakeConn{id: "host-s2", role: RoleHost, sessionID: "s2", scope: ShareScopeControl}
	if err := hub.RegisterHost(host, "s1", 80, 24); err != nil {
		t.Fatalf("RegisterHost: %v", err)
	}
	if err := hub.RegisterHost(peer, "s2", 80, 24); err != nil {
		t.Fatalf("RegisterHost peer: %v", err)
	}

	svc := newWallService(store, hub, nil, 2*time.Second, []time.Duration{25 * time.Millisecond})
	enabled, _ := svc.setInactivity("alice", "s1", "alice@127.0.0.1", true, now)
	if !enabled {
		t.Fatalf("setInactivity should enable monitor")
	}

	waitFor := func(want int) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if len(host.sent) >= want && len(peer.sent) >= want {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %d frames, got host=%d peer=%d", want, len(host.sent), len(peer.sent))
	}

	waitFor(1)
	first := len(host.sent)
	firstPeer := len(peer.sent)
	if firstPeer != first {
		t.Fatalf("peer frames = %d, want %d", firstPeer, first)
	}
	if got := peer.sent[0].GetWall(); got == nil || got.Message != "session-a inactive" {
		t.Fatalf("unexpected peer wall frame: %#v", peer.sent[0].GetWall())
	}
	if got := peer.sent[0].GetWall(); got == nil || got.GetKind() != protocolpb.WallKind_WALL_KIND_INACTIVITY {
		t.Fatalf("expected inactivity wall kind, got %#v", peer.sent[0].GetWall())
	}
	time.Sleep(80 * time.Millisecond)
	if len(host.sent) != first || len(peer.sent) != firstPeer {
		t.Fatalf("expected one inactivity wall while enabled, got host=%d peer=%d", len(host.sent), len(peer.sent))
	}

	svc.markActivity("s1", time.Now().UTC())
	waitFor(first + 1)
}

func TestWallServiceToggleInactivityCyclesLevelsThenOff(t *testing.T) {
	store := NewStore()
	hub := NewHub(nil)
	now := time.Now().UTC()
	store.CreateSession(Session{ID: "s1", Username: "alice", Status: "active", CreatedAt: now, LastActiveAt: now})

	svc := newWallService(store, hub, nil, 2*time.Second, []time.Duration{2 * time.Minute, 5 * time.Minute, 15 * time.Minute})

	enabled, after := svc.toggleInactivity("alice", "s1", "alice@127.0.0.1", now)
	if !enabled || after != 2*time.Minute {
		t.Fatalf("first toggle = enabled %v after %v, want enabled true after 2m", enabled, after)
	}
	enabled, after = svc.toggleInactivity("alice", "s1", "alice@127.0.0.1", now)
	if !enabled || after != 5*time.Minute {
		t.Fatalf("second toggle = enabled %v after %v, want enabled true after 5m", enabled, after)
	}
	enabled, after = svc.toggleInactivity("alice", "s1", "alice@127.0.0.1", now)
	if !enabled || after != 15*time.Minute {
		t.Fatalf("third toggle = enabled %v after %v, want enabled true after 15m", enabled, after)
	}
	enabled, after = svc.toggleInactivity("alice", "s1", "alice@127.0.0.1", now)
	if enabled || after != 0 {
		t.Fatalf("fourth toggle = enabled %v after %v, want enabled false after 0", enabled, after)
	}
}
