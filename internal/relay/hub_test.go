package relay

import (
	"context"
	"testing"

	"google.golang.org/protobuf/proto"
	"pkt.systems/lingon/internal/config"
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

func TestHubRegisterClientReplacesSameClientID(t *testing.T) {
	hub := NewHub(nil)
	host := &fakeConn{id: "host", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
	if err := hub.RegisterHost(host, "s1", 80, 24); err != nil {
		t.Fatalf("RegisterHost: %v", err)
	}

	oldConn := &fakeConn{id: "conn-old", role: RoleClient, sessionID: "s1", scope: ShareScopeControl}
	granted, holder, _, _ := hub.RegisterClient(oldConn, "s1", "android-1", true)
	if !granted {
		t.Fatalf("expected initial client to be granted control")
	}
	if holder != "android-1" {
		t.Fatalf("holder = %q, want android-1", holder)
	}
	if got := hub.ClientCount("s1"); got != 1 {
		t.Fatalf("client count = %d, want 1", got)
	}
	if oldConn.closed != 0 {
		t.Fatalf("old connection should remain open before reconnect, closed=%d", oldConn.closed)
	}

	newConn := &fakeConn{id: "conn-new", role: RoleClient, sessionID: "s1", scope: ShareScopeControl}
	granted, holder, _, _ = hub.RegisterClient(newConn, "s1", "android-1", true)
	if !granted {
		t.Fatalf("expected reconnecting client to be granted control")
	}
	if holder != "android-1" {
		t.Fatalf("holder = %q, want android-1", holder)
	}
	if got := hub.ClientCount("s1"); got != 1 {
		t.Fatalf("client count = %d, want 1 after reconnect replace", got)
	}
	if !hub.HasClientID("s1", "android-1") {
		t.Fatalf("expected client ID to remain registered")
	}
	if oldConn.closed != 0 {
		t.Fatalf("RegisterClient should not close the replaced client directly; closed=%d", oldConn.closed)
	}
	if got := hub.ControllerID("s1"); got != "android-1" {
		t.Fatalf("controller = %q, want android-1", got)
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
	if got := hub.replayHistoryBytes; got != config.DefaultReplayHistoryBytes {
		t.Fatalf("default replayHistoryBytes = %d, want %d", got, config.DefaultReplayHistoryBytes)
	}
	host := &fakeConn{id: "host", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
	hub.RegisterHost(host, "s1", 80, 24)
	client := &fakeConn{id: "client", role: RoleClient, sessionID: "s1", scope: ShareScopeControl}
	_, _, _, _ = hub.RegisterClient(client, "s1", "client", false)

	frame := frameWall("s1", 42, "alice@127.0.0.1", "hello", 5, protocolpb.WallKind_WALL_KIND_UNSPECIFIED, "")
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

func TestHubReplayHistoryBytesSetterTrimsHistory(t *testing.T) {
	hub := NewHub(nil)
	hub.SetReplayHistoryBytes(1)

	host := &fakeConn{id: "host", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
	if err := hub.RegisterHost(host, "s1", 80, 24); err != nil {
		t.Fatalf("RegisterHost: %v", err)
	}

	if err := hub.HandleHostFrame(context.Background(), host, hostSnapshotFrame("alpha")); err != nil {
		t.Fatalf("HandleHostFrame(snapshot): %v", err)
	}
	if err := hub.HandleHostFrame(context.Background(), host, hostDiffFrame("beta")); err != nil {
		t.Fatalf("HandleHostFrame(diff): %v", err)
	}

	state := hub.session("s1")
	if state == nil {
		t.Fatal("expected session state")
	}
	if got := hub.replayHistoryBytes; got != 1 {
		t.Fatalf("replayHistoryBytes = %d, want 1", got)
	}
	if len(state.history) != 1 {
		t.Fatalf("history length = %d, want 1 after trim", len(state.history))
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

func TestHubReplaysMissingFramesToReconnectingClient(t *testing.T) {
	hub := NewHub(nil)
	host := &fakeConn{id: "host", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
	if err := hub.RegisterHost(host, "s1", 80, 24); err != nil {
		t.Fatalf("RegisterHost: %v", err)
	}

	if err := hub.HandleHostFrame(context.Background(), host, hostSnapshotFrame("alpha-1")); err != nil {
		t.Fatalf("HandleHostFrame(snapshot): %v", err)
	}
	if err := hub.HandleHostFrame(context.Background(), host, hostDiffFrame("alpha-2")); err != nil {
		t.Fatalf("HandleHostFrame(diff1): %v", err)
	}
	if err := hub.HandleHostFrame(context.Background(), host, hostDiffFrame("alpha-3")); err != nil {
		t.Fatalf("HandleHostFrame(diff2): %v", err)
	}

	client := &fakeConn{id: "client", role: RoleClient, sessionID: "s1", scope: ShareScopeControl}
	_, _, _, _ = hub.RegisterClient(client, "s1", "client", false)

	hello := &protocolpb.Frame{
		SessionId: "s1",
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			ClientId:     "client",
			Cols:         80,
			Rows:         24,
			WantsControl: false,
			LastSeq:      2,
			ClientType:   "android",
		}},
	}
	if err := hub.HandleClientFrame(context.Background(), client, hello); err != nil {
		t.Fatalf("HandleClientFrame(hello): %v", err)
	}

	if len(host.sent) != 0 {
		t.Fatalf("expected hello replay path to skip forwarding to host; sent=%d", len(host.sent))
	}
	if len(client.sent) != 1 {
		t.Fatalf("expected 1 replay frame, got %d", len(client.sent))
	}
	if got := client.sent[0].Seq; got != 3 {
		t.Fatalf("replay seq = %d, want 3", got)
	}
	if got := client.sent[0].GetDiff(); got == nil || got.Title != "alpha-3" {
		t.Fatalf("replay diff = %+v, want title alpha-3", got)
	}
}

func TestHubFallsBackToHelloWhenReplayHistoryIsTooOld(t *testing.T) {
	hub := NewHub(nil)
	host := &fakeConn{id: "host", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
	if err := hub.RegisterHost(host, "s1", 80, 24); err != nil {
		t.Fatalf("RegisterHost: %v", err)
	}

	state := hub.session("s1")
	state.seq = 10
	state.history = []*protocolpb.Frame{
		hostSnapshotFrame("alpha-9"),
		hostDiffFrame("alpha-10"),
	}
	state.history[0].Seq = 9
	state.history[1].Seq = 10
	state.historyBytes = proto.Size(state.history[0]) + proto.Size(state.history[1])

	client := &fakeConn{id: "client", role: RoleClient, sessionID: "s1", scope: ShareScopeControl}
	_, _, _, _ = hub.RegisterClient(client, "s1", "client", false)

	hello := &protocolpb.Frame{
		SessionId: "s1",
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			ClientId:     "client",
			Cols:         80,
			Rows:         24,
			WantsControl: false,
			LastSeq:      7,
			ClientType:   "android",
		}},
	}
	if err := hub.HandleClientFrame(context.Background(), client, hello); err != nil {
		t.Fatalf("HandleClientFrame(hello): %v", err)
	}

	if len(client.sent) != 0 {
		t.Fatalf("expected no replay frames when history is too old, got %d", len(client.sent))
	}
	if len(host.sent) != 1 {
		t.Fatalf("expected hello to be forwarded to host, got %d", len(host.sent))
	}
	if got := host.sent[0].GetHello(); got == nil || got.LastSeq != 7 {
		t.Fatalf("hello forwarded with last_seq=%+v, want 7", got)
	}
}

func TestHubClearsReplayHistoryWhenHostIsReplaced(t *testing.T) {
	hub := NewHub(nil)
	hostA := &fakeConn{id: "host-a", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
	hostB := &fakeConn{id: "host-b", role: RoleHost, sessionID: "s1", scope: ShareScopeControl}
	if err := hub.RegisterHost(hostA, "s1", 80, 24); err != nil {
		t.Fatalf("RegisterHost(hostA): %v", err)
	}

	if err := hub.HandleHostFrame(context.Background(), hostA, hostSnapshotFrame("alpha-1")); err != nil {
		t.Fatalf("HandleHostFrame(snapshot): %v", err)
	}
	if err := hub.HandleHostFrame(context.Background(), hostA, hostDiffFrame("alpha-2")); err != nil {
		t.Fatalf("HandleHostFrame(diff): %v", err)
	}

	replaced := hub.registerHost(hostB, "s1", 80, 24)
	if replaced == nil || replaced.ID() != hostA.ID() {
		t.Fatalf("registerHost(hostB): replaced=%v, want host-a", replaced)
	}

	state := hub.session("s1")
	if len(state.history) != 0 {
		t.Fatalf("history length after takeover = %d, want 0", len(state.history))
	}
	if state.historyBytes != 0 {
		t.Fatalf("historyBytes after takeover = %d, want 0", state.historyBytes)
	}

	client := &fakeConn{id: "client", role: RoleClient, sessionID: "s1", scope: ShareScopeControl}
	_, _, _, _ = hub.RegisterClient(client, "s1", "client", false)

	hello := &protocolpb.Frame{
		SessionId: "s1",
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			ClientId:     "client",
			Cols:         80,
			Rows:         24,
			WantsControl: false,
			LastSeq:      2,
			ClientType:   "android",
		}},
	}
	if err := hub.HandleClientFrame(context.Background(), client, hello); err != nil {
		t.Fatalf("HandleClientFrame(hello): %v", err)
	}

	if len(client.sent) != 0 {
		t.Fatalf("expected no stale replay after host takeover, got %d frames", len(client.sent))
	}
	if len(hostB.sent) != 1 {
		t.Fatalf("expected hello to be forwarded to replacement host, got %d frames", len(hostB.sent))
	}
	if got := hostB.sent[0].GetHello(); got == nil || got.LastSeq != 2 {
		t.Fatalf("forwarded hello last_seq=%+v, want 2", got)
	}
}

func hostSnapshotFrame(title string) *protocolpb.Frame {
	return &protocolpb.Frame{
		SessionId: "s1",
		Payload: &protocolpb.Frame_Snapshot{Snapshot: &protocolpb.Snapshot{
			Cols:  80,
			Rows:  24,
			Title: title,
		}},
	}
}

func hostDiffFrame(title string) *protocolpb.Frame {
	return &protocolpb.Frame{
		SessionId: "s1",
		Payload: &protocolpb.Frame_Diff{Diff: &protocolpb.Diff{
			Title: title,
		}},
	}
}
