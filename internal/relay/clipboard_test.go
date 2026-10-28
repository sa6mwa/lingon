package relay

import (
	"context"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/protocolpb"
)

func clipboardFrame(offset uint32) *protocolpb.Frame {
	return &protocolpb.Frame{SessionId: "wrong-session", Payload: &protocolpb.Frame_Clipboard{Clipboard: &protocolpb.Clipboard{TransferId: strings.Repeat("a", 32), Source: "spoofed", Mime: "image/png", Total: 100, Offset: offset, Data: []byte{1, 2, 3, 4}}}}
}

func TestClipboardRelayAuthorizationAndEphemeralReplies(t *testing.T) {
	ctx := context.Background()
	h := NewHub(nil)
	host := &fakeConn{id: "host", sessionID: "s", role: RoleHost, scope: ShareScopeControl}
	_ = h.RegisterHost(host, "s", 80, 24)
	owner := &fakeConn{id: "owner", sessionID: "s", role: RoleClient, scope: ShareScopeControl}
	viewer := &fakeConn{id: "viewer", sessionID: "s", role: RoleClient, scope: ShareScopeView}
	h.RegisterClient(owner, "s", "owner-friendly", false)
	h.RegisterClient(viewer, "s", "viewer-friendly", false)
	if err := h.HandleClientFrame(ctx, viewer, clipboardFrame(0)); err == nil {
		t.Fatal("viewer sent image")
	}
	if err := h.HandleClientFrame(ctx, owner, clipboardFrame(0)); err != nil {
		t.Fatal(err)
	}
	got := host.sentFrame(host.sentLen() - 1)
	if got.SessionId != "s" || got.Seq != 0 || got.GetClipboard().Source != "owner" {
		t.Fatal("untrusted clipboard identity forwarded", got)
	}
	hostAck := &protocolpb.Frame{Payload: &protocolpb.Frame_Clipboard{Clipboard: &protocolpb.Clipboard{TransferId: strings.Repeat("a", 32), Source: "owner", Completed: true}}}
	viewerBefore := viewer.sentLen()
	ownerBefore := owner.sentLen()
	if err := h.HandleHostFrame(ctx, host, hostAck); err != nil {
		t.Fatal(err)
	}
	if viewer.sentLen() != viewerBefore || owner.sentLen() != ownerBefore+1 {
		t.Fatal("clipboard reply leaked to another client")
	}
	h.mu.Lock()
	history := len(h.sessions["s"].history)
	h.mu.Unlock()
	if history != 0 {
		t.Fatal("clipboard entered replay history")
	}
	bad := clipboardFrame(0)
	bad.GetClipboard().Completed = true
	if err := h.HandleClientFrame(ctx, owner, bad); err == nil {
		t.Fatal("client forged response")
	}
	bad = clipboardFrame(0)
	bad.GetClipboard().Total = 33 << 20
	if err := h.HandleClientFrame(ctx, owner, bad); err == nil {
		t.Fatal("oversized transfer accepted")
	}
}

func TestClipboardRelayRevocationAndControlChangeFence(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "control change", true: "revoke"}[revoke], func(t *testing.T) {
			ctx := context.Background()
			h := NewHub(nil)
			host := &fakeConn{id: "host", sessionID: "s", role: RoleHost, scope: ShareScopeControl}
			_ = h.RegisterHost(host, "s", 80, 24)
			a := &fakeConn{id: "a", sessionID: "s", role: RoleClient, scope: ShareScopeControl, shareToken: "share"}
			b := &fakeConn{id: "b", sessionID: "s", role: RoleClient, scope: ShareScopeControl}
			h.RegisterClient(a, "s", "a", false)
			h.RegisterClient(b, "s", "b", false)
			if err := h.HandleClientFrame(ctx, a, clipboardFrame(0)); err != nil {
				t.Fatal(err)
			}
			if revoke {
				if h.DisconnectShareToken("share", "revoked") != 1 {
					t.Fatal("share not revoked")
				}
				last := host.sentFrame(host.sentLen() - 1).GetClipboard()
				if last == nil || !last.Cancel || last.Source != "a" {
					t.Fatal("partial revoked upload not discarded")
				}
			} else {
				if err := h.HandleClientFrame(ctx, b, &protocolpb.Frame{SessionId: "s", Payload: &protocolpb.Frame_In{In: &protocolpb.In{Data: []byte("b")}}}); err != nil {
					t.Fatal(err)
				}
			}
			before := host.sentLen()
			if err := h.HandleClientFrame(ctx, a, clipboardFrame(4)); err == nil {
				t.Fatal("upload survived revocation/control loss")
			}
			if host.sentLen() != before {
				t.Fatal("unauthorized bytes reached host")
			}
		})
	}
}

func TestClipboardPermissionFailureAcknowledged(t *testing.T) {
	f := newShareHandshakeFixture(t, ShareScopeView)
	conn := f.dial(t, "token")
	f.write(t, conn, &protocolpb.Frame{Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{ClientId: "clipboard-viewer", ClientType: "cli"}}})
	for {
		frame, err := readFrame(f.ctx, conn, wsReadLimit)
		if err != nil {
			t.Fatal(err)
		}
		if frame.GetWelcome() != nil {
			break
		}
	}
	request := clipboardFrame(0)
	request.SessionId = ""
	f.write(t, conn, request)
	for {
		frame, err := readFrame(f.ctx, conn, wsReadLimit)
		if err != nil {
			t.Fatal(err)
		}
		if frame.GetError() != nil {
			t.Fatal("clipboard error was not acknowledged to the upload", frame)
		}
		if reply := frame.GetClipboard(); reply != nil {
			if !reply.Completed || reply.Error != "control not permitted" || reply.TransferId != request.GetClipboard().TransferId || len(reply.Data) != 0 {
				t.Fatal("wrong clipboard failure", reply)
			}
			break
		}
	}
	for i := 0; i < f.host.sentLen(); i++ {
		if f.host.sentFrame(i).GetClipboard() != nil {
			t.Fatal("view-only clipboard data reached host")
		}
	}
}
