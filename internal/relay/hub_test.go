package relay

import (
	"context"
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
	hub.RegisterHost(host, "s1", 80, 24)

	client := &fakeConn{id: "client", role: RoleClient, sessionID: "s1", scope: ShareScopeView}
	_, _, _, _ = hub.RegisterClient(client, "s1", "client", false)

	frame := &protocolpb.Frame{SessionId: "s1", Payload: &protocolpb.Frame_In{In: &protocolpb.In{Data: []byte("hi")}}}
	if err := hub.HandleClientFrame(context.Background(), client, frame); err == nil {
		t.Fatalf("expected error for view-only client")
	}
}

func TestHubControlTakesLeaseOnCommand(t *testing.T) {
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

	frame := &protocolpb.Frame{
		SessionId: "s1",
		Payload: &protocolpb.Frame_Command{Command: &protocolpb.Command{
			Kind: protocolpb.CommandKind_COMMAND_KIND_SEND_EOF,
		}},
	}
	if err := hub.HandleClientFrame(context.Background(), client, frame); err != nil {
		t.Fatalf("HandleClientFrame: %v", err)
	}

	controller, _, _, _ := hub.SessionState("s1")
	if controller != client.ID() {
		t.Fatalf("controller = %q, want %q", controller, client.ID())
	}
}

func TestHubViewOnlyDeniedCommand(t *testing.T) {
	hub := NewHub(nil)
	host := &fakeConn{id: "host", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
	hub.RegisterHost(host, "s1", 80, 24)

	client := &fakeConn{id: "client", role: RoleClient, sessionID: "s1", scope: ShareScopeView}
	_, _, _, _ = hub.RegisterClient(client, "s1", "client", false)

	frame := &protocolpb.Frame{
		SessionId: "s1",
		Payload: &protocolpb.Frame_Command{Command: &protocolpb.Command{
			Kind: protocolpb.CommandKind_COMMAND_KIND_SEND_EOF,
		}},
	}
	if err := hub.HandleClientFrame(context.Background(), client, frame); err == nil {
		t.Fatalf("expected error for view-only client command")
	}
}

func TestHubBroadcastFromHost(t *testing.T) {
	hub := NewHub(nil)
	host := &fakeConn{id: "host", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
	hub.RegisterHost(host, "s1", 80, 24)

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
	hub.RegisterHost(host, "s1", 80, 24)
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

func TestHubRegisterHostReplacesActiveHost(t *testing.T) {
	hub := NewHub(nil)
	hostA := &fakeConn{id: "host-a", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
	hostB := &fakeConn{id: "host-b", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}

	if err := hub.RegisterHost(hostA, "s1", 80, 24); err != nil {
		t.Fatalf("RegisterHost(hostA): %v", err)
	}
	replaced := hub.registerHost(hostB, "s1", 120, 40)
	if replaced == nil || replaced.ID() != hostA.ID() {
		t.Fatalf("RegisterHost(hostB): replaced=%v, want host-a", replaced)
	}
	if hostA.closed != 0 {
		t.Fatalf("hub should not close replaced host directly; closed=%d", hostA.closed)
	}
	if hostB.closed != 0 {
		t.Fatalf("expected new host to remain untouched by hub; closed=%d", hostB.closed)
	}
	_, cols, rows, _ := hub.SessionState("s1")
	if cols != 120 || rows != 40 {
		t.Fatalf("session size should reflect takeover host dimensions: cols=%d rows=%d", cols, rows)
	}
}

func TestHubHandleHostFrameRejectsStaleHost(t *testing.T) {
	hub := NewHub(nil)
	hostA := &fakeConn{id: "host-a", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
	hostB := &fakeConn{id: "host-b", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
	client := &fakeConn{id: "client", role: RoleClient, sessionID: "s1", scope: ShareScopeControl}

	if err := hub.RegisterHost(hostA, "s1", 80, 24); err != nil {
		t.Fatalf("RegisterHost(hostA): %v", err)
	}
	_, _, _, _ = hub.RegisterClient(client, "s1", "client", false)
	hub.registerHost(hostB, "s1", 120, 40)

	frame := &protocolpb.Frame{SessionId: "s1", Payload: &protocolpb.Frame_Out{Out: &protocolpb.Out{Data: []byte("stale")}}}
	if err := hub.HandleHostFrame(context.Background(), hostA, frame); err != errStaleHostConnection {
		t.Fatalf("stale host frame err = %v, want %v", err, errStaleHostConnection)
	}
	if len(client.sent) != 0 {
		t.Fatalf("stale host should not broadcast frames; got %d", len(client.sent))
	}
}
