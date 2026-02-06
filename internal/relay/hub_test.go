package relay

import (
	"context"
	"errors"
	"testing"

	"pkt.systems/lingon/internal/protocolpb"
)

type fakeConn struct {
	id        string
	role      Role
	sessionID string
	scope     ShareScope
	sent      []*protocolpb.Frame
	closed    int
}

func (f *fakeConn) ID() string        { return f.id }
func (f *fakeConn) Role() Role        { return f.role }
func (f *fakeConn) Scope() ShareScope { return f.scope }
func (f *fakeConn) SessionID() string { return f.sessionID }
func (f *fakeConn) Send(ctx context.Context, frame *protocolpb.Frame) error {
	f.sent = append(f.sent, frame)
	return nil
}
func (f *fakeConn) Close(ctx context.Context, reason string) error {
	f.closed++
	return nil
}

func TestHubControlTakesLeaseOnInput(t *testing.T) {
	hub := NewHub(nil)
	host := &fakeConn{id: "host", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
	if err := hub.RegisterHost(host, "s1", 80, 24); err != nil {
		t.Fatalf("RegisterHost: %v", err)
	}

	client := &fakeConn{id: "client", role: RoleClient, sessionID: "s1", scope: ShareScopeControl}
	granted, _, _, _ := hub.RegisterClient(client, "s1", "client", false)
	if granted {
		t.Fatalf("unexpected control on register")
	}

	frame := &protocolpb.Frame{SessionId: "s1", Payload: &protocolpb.Frame_In{In: &protocolpb.In{Data: []byte("hi")}}}
	if err := hub.HandleClientFrame(context.Background(), client, frame); err != nil {
		t.Fatalf("HandleClientFrame: %v", err)
	}

	controller, _, _, _ := hub.SessionState("s1")
	if controller != client.ID() {
		t.Fatalf("controller = %q, want %q", controller, client.ID())
	}
	if len(client.sent) == 0 {
		t.Fatalf("expected control broadcast")
	}
}

func TestHubViewOnlyDeniedControl(t *testing.T) {
	hub := NewHub(nil)
	host := &fakeConn{id: "host", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
	_ = hub.RegisterHost(host, "s1", 80, 24)

	client := &fakeConn{id: "client", role: RoleClient, sessionID: "s1", scope: ShareScopeView}
	_, _, _, _ = hub.RegisterClient(client, "s1", "client", false)

	frame := &protocolpb.Frame{SessionId: "s1", Payload: &protocolpb.Frame_In{In: &protocolpb.In{Data: []byte("hi")}}}
	if err := hub.HandleClientFrame(context.Background(), client, frame); err == nil {
		t.Fatalf("expected error for view-only client")
	}
}

func TestHubBroadcastFromHost(t *testing.T) {
	hub := NewHub(nil)
	host := &fakeConn{id: "host", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
	_ = hub.RegisterHost(host, "s1", 80, 24)

	client := &fakeConn{id: "client", role: RoleClient, sessionID: "s1", scope: ShareScopeControl}
	_, _, _, _ = hub.RegisterClient(client, "s1", "client", false)

	frame := &protocolpb.Frame{SessionId: "s1", Payload: &protocolpb.Frame_Out{Out: &protocolpb.Out{Data: []byte("out")}}}
	if err := hub.HandleHostFrame(context.Background(), host, frame); err != nil {
		t.Fatalf("HandleHostFrame: %v", err)
	}
	if len(client.sent) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(client.sent))
	}
	if client.sent[0].Seq == 0 {
		t.Fatalf("expected seq to be set")
	}
}

func TestHubBroadcastSessionFrame(t *testing.T) {
	hub := NewHub(nil)
	host := &fakeConn{id: "host", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
	_ = hub.RegisterHost(host, "s1", 80, 24)
	client := &fakeConn{id: "client", role: RoleClient, sessionID: "s1", scope: ShareScopeControl}
	_, _, _, _ = hub.RegisterClient(client, "s1", "client", false)

	frame := frameWall("s1", "alice@127.0.0.1", "hello", 5)
	if !hub.BroadcastSessionFrame(context.Background(), "s1", frame, true) {
		t.Fatalf("expected broadcast success")
	}
	if len(client.sent) != 1 {
		t.Fatalf("client frames = %d, want 1", len(client.sent))
	}
	if len(host.sent) != 1 {
		t.Fatalf("host frames = %d, want 1", len(host.sent))
	}
	if client.sent[0].Seq == 0 || host.sent[0].Seq == 0 {
		t.Fatalf("expected assigned frame sequence")
	}
}

func TestHubRegisterHostRejectsActiveHostOverride(t *testing.T) {
	hub := NewHub(nil)
	hostA := &fakeConn{id: "host-a", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
	hostB := &fakeConn{id: "host-b", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}

	if err := hub.RegisterHost(hostA, "s1", 80, 24); err != nil {
		t.Fatalf("RegisterHost(hostA): %v", err)
	}
	err := hub.RegisterHost(hostB, "s1", 120, 40)
	if !errors.Is(err, errSessionHasActiveHost) {
		t.Fatalf("RegisterHost(hostB): got %v, want %v", err, errSessionHasActiveHost)
	}
	if hostA.closed != 0 {
		t.Fatalf("expected existing host to remain connected; closed=%d", hostA.closed)
	}
	if hostB.closed != 0 {
		t.Fatalf("expected rejected host to remain untouched by hub; closed=%d", hostB.closed)
	}
	_, cols, rows, _ := hub.SessionState("s1")
	if cols != 80 || rows != 24 {
		t.Fatalf("session size mutated on rejected override: cols=%d rows=%d", cols, rows)
	}
}
