package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
	"pkt.systems/lingon/internal/protocolpb"
)

func TestShareRevocationRejectsPendingHello(t *testing.T) {
	for _, action := range []string{"revoke", "revoke-all", "credentials"} {
		for _, route := range []string{"token", "cookie", "forced-cookie"} {
			for _, scope := range []ShareScope{ShareScopeView, ShareScopeControl} {
				t.Run(action+"/"+route+"/"+string(scope), func(t *testing.T) {
					fixture := newShareHandshakeFixture(t, scope)
					conn := fixture.dial(t, route)
					// Dial has completed HTTP upgrade, but the server still
					// awaits hello and has not registered this client.
					fixture.revoke(t, action)
					fixture.write(t, conn, &protocolpb.Frame{Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
						ClientId: "pending-revoked", ClientType: "cli", WantsControl: true,
					}}})
					// Queue input as well: rejection must prevent both session
					// admission and terminal input, regardless of share scope.
					data, err := proto.Marshal(&protocolpb.Frame{Payload: &protocolpb.Frame_In{In: &protocolpb.In{Data: []byte("revoked-input")}}})
					if err != nil {
						t.Fatal(err)
					}
					_ = conn.Write(fixture.ctx, websocket.MessageBinary, data)
					for {
						frame, err := readFrame(fixture.ctx, conn, wsReadLimit)
						if err != nil {
							if fixture.ctx.Err() != nil {
								t.Fatal("revoked handshake was not rejected before transport timeout")
							}
							break
						}
						if frame.GetError() == nil {
							t.Fatalf("revoked pending handshake received session data: %v", frame)
						}
						if frame.GetError().GetMessage() != "invalid share token" {
							t.Fatalf("pending hello was not rejected as unauthorized: %v", frame)
						}
					}
					if got := fixture.server.Hub.ClientCount(fixture.sessionID); got != 0 {
						t.Fatalf("revoked handshake registered %d clients", got)
					}
					for i := 0; i < fixture.host.sentLen(); i++ {
						if fixture.host.sentFrame(i).GetIn() != nil {
							t.Fatal("revoked pending handshake forwarded terminal input")
						}
					}
				})
			}
		}
	}
}

func TestSharePendingHelloSurvivesUnrelatedRevocation(t *testing.T) {
	for _, route := range []string{"token", "cookie", "forced-cookie"} {
		t.Run(route, func(t *testing.T) {
			f := newShareHandshakeFixture(t, ShareScopeControl)
			conn := f.dial(t, route)
			other := *f
			var err error
			other.share, err = f.server.Store.CreateShareToken(f.sessionID, ShareScopeControl, time.Hour, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			other.revoke(t, "revoke")
			f.write(t, conn, &protocolpb.Frame{Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
				ClientId: "still-authorized", ClientType: "cli", WantsControl: true,
			}}})
			frame, err := readFrame(f.ctx, conn, wsReadLimit)
			if err != nil || frame.GetWelcome() == nil || !frame.GetWelcome().GetGrantedControl() {
				t.Fatalf("unrelated revocation rejected a valid handshake: %v %v", frame, err)
			}
			f.write(t, conn, &protocolpb.Frame{Payload: &protocolpb.Frame_In{In: &protocolpb.In{Data: []byte("valid-input")}}})
			select {
			case input := <-f.host.inputs:
				if input != "valid-input" {
					t.Fatalf("forwarded input = %q", input)
				}
			case <-f.ctx.Done():
				t.Fatal("valid share lost terminal input after unrelated revocation")
			}
		})
	}
}

func TestShareRegistrationIsFencedAgainstConcurrentRevocation(t *testing.T) {
	f := newShareHandshakeFixture(t, ShareScopeControl)
	for range 200 {
		share, err := f.server.Store.CreateShareToken(f.sessionID, ShareScopeControl, time.Hour, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		ws := &wsConn{id: newConnID(), role: RoleClient, sessionID: f.sessionID, scope: ShareScopeControl, shareToken: share.Token, done: make(chan struct{})}
		req := httptest.NewRequest(http.MethodGet, "/ws/client?token="+url.QueryEscape(share.Token), nil)
		start := make(chan struct{})
		registered, revoked := make(chan error, 1), make(chan error, 1)
		go func() {
			<-start
			_, err := f.server.registerSessionClient(ws, req, "racing-share", true)
			registered <- err
		}()
		go func() {
			<-start
			err := f.server.Store.RevokeShareToken(share.Token, time.Now().UTC())
			if err == nil {
				f.server.disconnectShareToken(share.Token)
			}
			revoked <- err
		}()
		close(start)
		registrationErr, revocationErr := <-registered, <-revoked
		if revocationErr != nil {
			t.Fatal(revocationErr)
		}
		if registrationErr != nil && !errors.Is(registrationErr, ErrTokenNotFound) {
			t.Fatal(registrationErr)
		}
		if got := f.server.Hub.ClientCount(f.sessionID); got != 0 {
			t.Fatalf("revoked client survived concurrent registration: %d clients", got)
		}
		if registrationErr == nil {
			select {
			case <-ws.done:
			default:
				t.Fatal("admitted share was not disconnected by revocation")
			}
		}
	}
}

func TestShareRevocationClosesSocketsOutsideAdmissionLock(t *testing.T) {
	f := newShareHandshakeFixture(t, ShareScopeControl)
	blocked := &blockingShareClose{
		fakeConn: &fakeConn{id: "slow-share", role: RoleClient, sessionID: f.sessionID, scope: ShareScopeView, shareToken: f.share.Token},
		started:  make(chan struct{}), release: make(chan struct{}),
	}
	f.server.Hub.RegisterClient(blocked, f.sessionID, "slow-share", false)
	if err := f.server.Store.RevokeShareToken(f.share.Token, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	t.Cleanup(func() { close(blocked.release); <-closed })
	go func() { defer close(closed); f.server.disconnectShareToken(f.share.Token) }()
	<-blocked.started
	var err error
	f.share, err = f.server.Store.CreateShareToken(f.sessionID, ShareScopeControl, time.Hour, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	conn := f.dial(t, "token")
	f.write(t, conn, &protocolpb.Frame{Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{ClientId: "unblocked", WantsControl: true}}})
	frame, err := readFrame(f.ctx, conn, wsReadLimit)
	if err != nil || frame.GetWelcome() == nil {
		t.Fatalf("blocked socket close stalled unrelated share admission: %v %v", frame, err)
	}
}

type blockingShareClose struct {
	*fakeConn
	started, release chan struct{}
}

func (c *blockingShareClose) Close(ctx context.Context, reason string) error {
	close(c.started)
	<-c.release
	return c.fakeConn.Close(ctx, reason)
}

type handshakeHostConn struct {
	*fakeConn
	inputs chan string
}

func (h *handshakeHostConn) Send(ctx context.Context, frame *protocolpb.Frame) error {
	if err := h.fakeConn.Send(ctx, frame); err != nil {
		return err
	}
	if input := frame.GetIn(); input != nil {
		h.inputs <- string(input.Data)
	}
	return nil
}

type shareHandshakeFixture struct {
	ctx       context.Context
	server    *HTTPServer
	ts        *httptest.Server
	host      *handshakeHostConn
	access    AccessToken
	share     ShareToken
	sessionID string
}

func newShareHandshakeFixture(t *testing.T, scope ShareScope) *shareHandshakeFixture {
	t.Helper()
	store, users := NewStore(), NewUserStore()
	users.Upsert(User{Username: "share-owner"})
	now := time.Now().UTC()
	access, err := store.CreateAccessToken("share-owner", DefaultAccessTokenTTL, now)
	if err != nil {
		t.Fatal(err)
	}
	const sessionID = "pending-share-session"
	store.CreateSession(Session{ID: sessionID, Username: "share-owner", Status: "active", CreatedAt: now, LastActiveAt: now})
	share, err := store.CreateShareToken(sessionID, scope, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	server := NewHTTPServer(store, users, NewAuthenticator(users), nil, nil)
	host := &handshakeHostConn{
		fakeConn: &fakeConn{id: "pending-share-host", role: RoleHost, sessionID: sessionID, scope: ShareScopeControl},
		inputs:   make(chan string, 1),
	}
	if err := server.Hub.RegisterHost(host, sessionID, 80, 24); err != nil {
		t.Fatal(err)
	}
	ts := newTestServer(t, server.Handler())
	t.Cleanup(ts.Close)
	t.Cleanup(func() { server.Close("test cleanup") })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return &shareHandshakeFixture{ctx: ctx, server: server, ts: ts, host: host, access: access, share: share, sessionID: sessionID}
}

func (f *shareHandshakeFixture) dial(t *testing.T, route string) *websocket.Conn {
	t.Helper()
	path := "/ws/client"
	headers := make(http.Header)
	if route == "token" {
		path += "?token=" + url.QueryEscape(f.share.Token)
	} else {
		body, err := json.Marshal(shareAuthRequest{Token: f.share.Token})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/auth/share", bytes.NewReader(body))
		res := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("share cookie authentication: %d %s", res.Code, res.Body.String())
		}
		for _, cookie := range res.Result().Cookies() {
			if cookie.Name == shareCookieName {
				headers.Add("Cookie", cookie.String())
			}
		}
		if headers.Get("Cookie") == "" {
			t.Fatal("missing share cookie")
		}
		if route == "forced-cookie" {
			headers.Set("X-Lingon-Share-Session", "1")
			headers.Add("Cookie", (&http.Cookie{Name: accessCookieName, Value: f.access.Token}).String())
		}
	}
	conn, _, err := websocket.Dial(f.ctx, wsURL(f.ts.URL, path), &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func (f *shareHandshakeFixture) write(t *testing.T, conn *websocket.Conn, frame *protocolpb.Frame) {
	t.Helper()
	data, err := proto.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(f.ctx, websocket.MessageBinary, data); err != nil {
		t.Fatal(err)
	}
}

func (f *shareHandshakeFixture) revoke(t *testing.T, action string) {
	t.Helper()
	if action == "credentials" {
		f.server.InvalidateReloadedUsers([]string{f.access.Username}, time.Now().UTC())
		return
	}
	body, err := json.Marshal(shareRevokeRequest{Token: f.share.Token})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.ts.URL+"/share/"+action, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+f.access.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := f.ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("%s returned %d, want 200", action, res.StatusCode)
	}
}
