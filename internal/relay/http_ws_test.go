package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/pslog"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestHostConnectLogsReconnected(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)

	var buf lockedBuffer
	logger := pslog.NewWithOptions(context.Background(), &buf, pslog.Options{
		Mode:             pslog.ModeStructured,
		DisableTimestamp: true,
		NoColor:          true,
	})
	server := NewHTTPServer(store, users, auth, logger, nil)
	ts := newTestServer(t, server.Handler())
	defer ts.Close()

	access, err := store.CreateAccessToken(user.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	connect := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		conn, _, err := websocket.Dial(ctx, wsURL(ts.URL, "/ws/host"), &websocket.DialOptions{
			HTTPHeader: map[string][]string{"Authorization": {"Bearer " + access.Token}},
		})
		if err != nil {
			t.Fatalf("websocket dial: %v", err)
		}
		defer func() {
			_ = conn.Close(websocket.StatusNormalClosure, "bye")
		}()
		frame := &protocolpb.Frame{
			SessionId: "session_test",
			Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
				Cols:         80,
				Rows:         24,
				WantsControl: true,
				ClientType:   "host",
			}},
		}
		data, err := proto.Marshal(frame)
		if err != nil {
			t.Fatalf("marshal hello: %v", err)
		}
		if err := conn.Write(ctx, websocket.MessageBinary, data); err != nil {
			t.Fatalf("send hello: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	connect()
	connect()

	logs := buf.String()
	if strings.Count(logs, "\"msg\":\"relay.host.connect.done\"") < 2 {
		t.Fatalf("expected host connected logs, got %s", logs)
	}
	if !strings.Contains(logs, "\"reconnected\":true") {
		t.Fatalf("expected reconnected log, got %s", logs)
	}
}

func TestWSHostReconnectTakesOverDuplicateSessionID(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)
	ts := newTestServer(t, server.Handler())
	defer ts.Close()

	access, err := store.CreateAccessToken(user.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	sendHostHello := func(conn *websocket.Conn, sessionID string) {
		t.Helper()
		frame := &protocolpb.Frame{
			SessionId: sessionID,
			Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
				Cols:         80,
				Rows:         24,
				WantsControl: true,
				ClientType:   "host",
			}},
		}
		data, marshalErr := proto.Marshal(frame)
		if marshalErr != nil {
			t.Fatalf("marshal hello: %v", marshalErr)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if writeErr := conn.Write(ctx, websocket.MessageBinary, data); writeErr != nil {
			t.Fatalf("send hello: %v", writeErr)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hostA, _, err := websocket.Dial(ctx, wsURL(ts.URL, "/ws/host"), &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + access.Token}},
	})
	if err != nil {
		t.Fatalf("hostA dial: %v", err)
	}
	defer func() {
		_ = hostA.Close(websocket.StatusNormalClosure, "bye")
	}()
	sendHostHello(hostA, "session-dup")
	waitForActiveSessionCount(t, ts.URL, access.Token, 1, 2*time.Second)

	hostB, _, err := websocket.Dial(ctx, wsURL(ts.URL, "/ws/host"), &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + access.Token}},
	})
	if err != nil {
		t.Fatalf("hostB dial: %v", err)
	}
	defer func() {
		_ = hostB.Close(websocket.StatusNormalClosure, "bye")
	}()
	sendHostHello(hostB, "session-dup")
	waitForActiveSessionCount(t, ts.URL, access.Token, 1, 2*time.Second)

	readUntil := time.Now().Add(2 * time.Second)
	var supersededErr *protocolpb.Error
	for time.Now().Before(readUntil) {
		readCtx, readCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		_, message, err := hostA.Read(readCtx)
		readCancel()
		if err != nil {
			continue
		}
		var frame protocolpb.Frame
		if err := proto.Unmarshal(message, &frame); err != nil {
			t.Fatalf("decode superseded host frame: %v", err)
		}
		if frame.GetError() == nil {
			continue
		}
		supersededErr = frame.GetError()
		break
	}
	if supersededErr == nil {
		t.Fatalf("expected superseded error frame for old host")
	}
	if !supersededErr.GetSessionRejected() {
		t.Fatalf("expected session_rejected=true for superseded host")
	}
	if !strings.Contains(supersededErr.GetMessage(), "superseded by reconnect") {
		t.Fatalf("unexpected superseded host message: %q", supersededErr.GetMessage())
	}

	_ = hostA.Close(websocket.StatusNormalClosure, "bye")
	waitForActiveSessionCount(t, ts.URL, access.Token, 1, 2*time.Second)

	if !server.Hub.HasHost("session-dup") {
		t.Fatalf("expected replacement host to remain active")
	}

	_ = hostB.Close(websocket.StatusNormalClosure, "bye")
	waitForActiveSessionCount(t, ts.URL, access.Token, 0, 2*time.Second)
}

func TestClientConnectLogsReconnected(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)

	var buf lockedBuffer
	logger := pslog.NewWithOptions(context.Background(), &buf, pslog.Options{
		Mode:             pslog.ModeStructured,
		DisableTimestamp: true,
		NoColor:          true,
	})
	server := NewHTTPServer(store, users, auth, logger, nil)
	ts := newTestServer(t, server.Handler())
	defer ts.Close()

	access, err := store.CreateAccessToken(user.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hostConn, _, err := websocket.Dial(ctx, wsURL(ts.URL, "/ws/host"), &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + access.Token}},
	})
	if err != nil {
		t.Fatalf("host dial: %v", err)
	}
	hostHello := &protocolpb.Frame{
		SessionId: "session_client_test",
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			Cols:         80,
			Rows:         24,
			WantsControl: true,
			ClientType:   "host",
		}},
	}
	hostData, err := proto.Marshal(hostHello)
	if err != nil {
		t.Fatalf("marshal host hello: %v", err)
	}
	if err := hostConn.Write(ctx, websocket.MessageBinary, hostData); err != nil {
		t.Fatalf("send host hello: %v", err)
	}
	defer func() {
		_ = hostConn.Close(websocket.StatusNormalClosure, "bye")
	}()

	connectClient := func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer ccancel()
		conn, _, err := websocket.Dial(cctx, wsURL(ts.URL, "/ws/client"), &websocket.DialOptions{
			HTTPHeader: map[string][]string{"Authorization": {"Bearer " + access.Token}},
		})
		if err != nil {
			t.Fatalf("client dial: %v", err)
		}
		defer func() {
			_ = conn.Close(websocket.StatusNormalClosure, "bye")
		}()
		frame := &protocolpb.Frame{
			SessionId: "session_client_test",
			Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
				ClientId:     "client_test",
				WantsControl: true,
				ClientType:   "client",
			}},
		}
		data, err := proto.Marshal(frame)
		if err != nil {
			t.Fatalf("marshal client hello: %v", err)
		}
		if err := conn.Write(cctx, websocket.MessageBinary, data); err != nil {
			t.Fatalf("send client hello: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	connectClient()
	connectClient()

	logs := buf.String()
	if strings.Count(logs, "\"msg\":\"relay.client.connect.done\"") < 2 {
		t.Fatalf("expected client connected logs, got %s", logs)
	}
	if !strings.Contains(logs, "\"reconnected\":true") {
		t.Fatalf("expected client reconnected log, got %s", logs)
	}
}

func TestWSHostActivityFrameDoesNotForwardToClients(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)
	ts := newTestServer(t, server.Handler())
	defer ts.Close()

	access, err := store.CreateAccessToken(user.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	hostConn, _, err := websocket.Dial(ctx, wsURL(ts.URL, "/ws/host"), &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + access.Token}},
	})
	if err != nil {
		t.Fatalf("host dial: %v", err)
	}
	defer func() {
		_ = hostConn.Close(websocket.StatusNormalClosure, "bye")
	}()

	hostHello := &protocolpb.Frame{
		SessionId: "activity-session",
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			Cols:         80,
			Rows:         24,
			WantsControl: true,
			ClientType:   "host",
		}},
	}
	hostHelloData, err := proto.Marshal(hostHello)
	if err != nil {
		t.Fatalf("marshal host hello: %v", err)
	}
	if err := hostConn.Write(ctx, websocket.MessageBinary, hostHelloData); err != nil {
		t.Fatalf("send host hello: %v", err)
	}

	clientConn, _, err := websocket.Dial(ctx, wsURL(ts.URL, "/ws/client"), &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + access.Token}},
	})
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	defer func() {
		_ = clientConn.Close(websocket.StatusNormalClosure, "bye")
	}()

	clientHello := &protocolpb.Frame{
		SessionId: "activity-session",
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			ClientId:     "observer",
			Cols:         80,
			Rows:         24,
			WantsControl: false,
			ClientType:   "attach",
		}},
	}
	clientHelloData, err := proto.Marshal(clientHello)
	if err != nil {
		t.Fatalf("marshal client hello: %v", err)
	}
	if err := clientConn.Write(ctx, websocket.MessageBinary, clientHelloData); err != nil {
		t.Fatalf("send client hello: %v", err)
	}

	readCtx, readCancel := context.WithTimeout(context.Background(), time.Second)
	_, _, err = clientConn.Read(readCtx)
	readCancel()
	if err != nil {
		t.Fatalf("expected welcome frame: %v", err)
	}
	for {
		drainCtx, drainCancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
		_, _, err := clientConn.Read(drainCtx)
		drainCancel()
		if err != nil {
			break
		}
	}

	activityData, err := proto.Marshal(frameActivity("activity-session"))
	if err != nil {
		t.Fatalf("marshal activity frame: %v", err)
	}
	if err := hostConn.Write(ctx, websocket.MessageBinary, activityData); err != nil {
		t.Fatalf("send activity frame: %v", err)
	}
	state := server.Hub.sessions["activity-session"]
	if state == nil {
		t.Fatal("expected hub session state")
	}
	if len(state.history) != 0 {
		t.Fatalf("activity frame should not enter replay history, got %d frames", len(state.history))
	}
	if state.historyBytes != 0 {
		t.Fatalf("activity frame should not consume replay history bytes, got %d", state.historyBytes)
	}

	noFrameCtx, noFrameCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer noFrameCancel()
	_, message, err := clientConn.Read(noFrameCtx)
	if err == nil {
		var frame protocolpb.Frame
		if unmarshalErr := proto.Unmarshal(message, &frame); unmarshalErr != nil {
			t.Fatalf("unmarshal unexpected frame: %v", unmarshalErr)
		}
		t.Fatalf("unexpected forwarded frame: %#v", frame.Payload)
	}
}

func TestWSClientConnectsWithShareSessionCookie(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)
	ts := newTestServer(t, server.Handler())
	defer ts.Close()

	access, err := store.CreateAccessToken(user.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hostConn, _, err := websocket.Dial(ctx, wsURL(ts.URL, "/ws/host"), &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + access.Token}},
	})
	if err != nil {
		t.Fatalf("host dial: %v", err)
	}
	defer func() {
		_ = hostConn.Close(websocket.StatusNormalClosure, "bye")
	}()
	hostHello := &protocolpb.Frame{
		SessionId: "session_cookie_share",
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			Cols:         80,
			Rows:         24,
			WantsControl: true,
			ClientType:   "host",
		}},
	}
	hostData, err := proto.Marshal(hostHello)
	if err != nil {
		t.Fatalf("marshal host hello: %v", err)
	}
	if err := hostConn.Write(ctx, websocket.MessageBinary, hostData); err != nil {
		t.Fatalf("send host hello: %v", err)
	}

	share, err := store.CreateShareToken("session_cookie_share", ShareScopeView, time.Hour, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateShareToken: %v", err)
	}
	accountCookie := (&http.Cookie{
		Name:  accessCookieName,
		Value: access.Token,
		Path:  "/",
	}).String()
	shareBody, _ := json.Marshal(shareAuthRequest{Token: share.Token})
	shareReq := httptest.NewRequest(http.MethodPost, "/auth/share", bytes.NewReader(shareBody))
	shareResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(shareResp, shareReq)
	if shareResp.Code != http.StatusOK {
		t.Fatalf("share auth status = %d, want %d", shareResp.Code, http.StatusOK)
	}
	var shareCookie string
	for _, cookie := range shareResp.Result().Cookies() {
		if cookie.Name == shareCookieName {
			shareCookie = cookie.String()
			break
		}
	}
	if shareCookie == "" {
		t.Fatalf("expected share session cookie")
	}

	clientConn, _, err := websocket.Dial(ctx, wsURL(ts.URL, "/ws/client"), &websocket.DialOptions{
		HTTPHeader: map[string][]string{
			"Cookie": {accountCookie + "; " + shareCookie},
		},
	})
	if err != nil {
		t.Fatalf("client dial with share cookie: %v", err)
	}
	defer func() {
		_ = clientConn.Close(websocket.StatusNormalClosure, "bye")
	}()
	clientHello := &protocolpb.Frame{
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			ClientId:     "web-share",
			WantsControl: true,
			ClientType:   "web",
		}},
	}
	clientData, err := proto.Marshal(clientHello)
	if err != nil {
		t.Fatalf("marshal client hello: %v", err)
	}
	if err := clientConn.Write(ctx, websocket.MessageBinary, clientData); err != nil {
		t.Fatalf("send client hello: %v", err)
	}

	_, message, err := clientConn.Read(ctx)
	if err != nil {
		t.Fatalf("read welcome frame: %v", err)
	}
	var frame protocolpb.Frame
	if err := proto.Unmarshal(message, &frame); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	if frame.GetWelcome() == nil {
		t.Fatalf("expected welcome frame, got %+v", frame.Payload)
	}
	if frame.GetWelcome().GetGrantedControl() {
		t.Fatalf("share cookie with account cookie was granted control")
	}
}

func TestWSClientStaleShareCookieDoesNotOverrideAccountAuth(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)
	ts := newTestServer(t, server.Handler())
	defer ts.Close()

	access, err := store.CreateAccessToken(user.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}
	accountCookie := (&http.Cookie{
		Name:  accessCookieName,
		Value: access.Token,
		Path:  "/",
	}).String()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hostConn, _, err := websocket.Dial(ctx, wsURL(ts.URL, "/ws/host"), &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + access.Token}},
	})
	if err != nil {
		t.Fatalf("host dial: %v", err)
	}
	defer func() {
		_ = hostConn.Close(websocket.StatusNormalClosure, "bye")
	}()
	hostHello := &protocolpb.Frame{
		SessionId: "normal_session",
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			Cols:         80,
			Rows:         24,
			WantsControl: true,
			ClientType:   "host",
		}},
	}
	hostData, err := proto.Marshal(hostHello)
	if err != nil {
		t.Fatalf("marshal host hello: %v", err)
	}
	if err := hostConn.Write(ctx, websocket.MessageBinary, hostData); err != nil {
		t.Fatalf("send host hello: %v", err)
	}

	share, err := store.CreateShareToken("other_shared_session", ShareScopeView, time.Hour, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateShareToken: %v", err)
	}
	shareBody, _ := json.Marshal(shareAuthRequest{Token: share.Token})
	shareReq := httptest.NewRequest(http.MethodPost, "/auth/share", bytes.NewReader(shareBody))
	shareResp := httptest.NewRecorder()
	server.Handler().ServeHTTP(shareResp, shareReq)
	if shareResp.Code != http.StatusOK {
		t.Fatalf("share auth status = %d, want %d", shareResp.Code, http.StatusOK)
	}
	var shareCookie string
	for _, cookie := range shareResp.Result().Cookies() {
		if cookie.Name == shareCookieName {
			shareCookie = cookie.String()
			break
		}
	}
	if shareCookie == "" {
		t.Fatalf("expected share session cookie")
	}
	if err := store.RevokeShareToken(share.Token, time.Now().UTC()); err != nil {
		t.Fatalf("RevokeShareToken: %v", err)
	}

	clientConn, _, err := websocket.Dial(ctx, wsURL(ts.URL, "/ws/client"), &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Cookie": {accountCookie + "; " + shareCookie}},
	})
	if err != nil {
		t.Fatalf("client dial with stale share cookie: %v", err)
	}
	defer func() {
		_ = clientConn.Close(websocket.StatusNormalClosure, "bye")
	}()
	clientHello := &protocolpb.Frame{
		SessionId: "normal_session",
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			ClientId:     "android-normal",
			WantsControl: true,
			ClientType:   "android",
		}},
	}
	clientData, err := proto.Marshal(clientHello)
	if err != nil {
		t.Fatalf("marshal client hello: %v", err)
	}
	if err := clientConn.Write(ctx, websocket.MessageBinary, clientData); err != nil {
		t.Fatalf("send client hello: %v", err)
	}

	_, message, err := clientConn.Read(ctx)
	if err != nil {
		t.Fatalf("read welcome frame: %v", err)
	}
	var frame protocolpb.Frame
	if err := proto.Unmarshal(message, &frame); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	if frame.GetWelcome() == nil {
		t.Fatalf("expected welcome frame, got %+v", frame.Payload)
	}
	if !frame.GetWelcome().GetGrantedControl() {
		t.Fatalf("account auth with stale share cookie was not granted control")
	}
}

func TestWSClientConnectsWithShareQueryToken(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)
	ts := newTestServer(t, server.Handler())
	defer ts.Close()

	access, err := store.CreateAccessToken(user.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hostConn, _, err := websocket.Dial(ctx, wsURL(ts.URL, "/ws/host"), &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + access.Token}},
	})
	if err != nil {
		t.Fatalf("host dial: %v", err)
	}
	defer func() {
		_ = hostConn.Close(websocket.StatusNormalClosure, "bye")
	}()
	hostHello := &protocolpb.Frame{
		SessionId: "session_query_share",
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			Cols:         80,
			Rows:         24,
			WantsControl: true,
			ClientType:   "host",
		}},
	}
	hostData, err := proto.Marshal(hostHello)
	if err != nil {
		t.Fatalf("marshal host hello: %v", err)
	}
	if err := hostConn.Write(ctx, websocket.MessageBinary, hostData); err != nil {
		t.Fatalf("send host hello: %v", err)
	}

	share, err := store.CreateShareToken("session_query_share", ShareScopeView, time.Hour, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateShareToken: %v", err)
	}
	clientConn, _, err := websocket.Dial(ctx, wsURL(ts.URL, "/ws/client?token="+url.QueryEscape(share.Token)), nil)
	if err != nil {
		t.Fatalf("client dial with share query token: %v", err)
	}
	defer func() {
		_ = clientConn.Close(websocket.StatusNormalClosure, "bye")
	}()
	clientHello := &protocolpb.Frame{
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			ClientId:     "cli-share",
			WantsControl: true,
			ClientType:   "cli",
		}},
	}
	clientData, err := proto.Marshal(clientHello)
	if err != nil {
		t.Fatalf("marshal client hello: %v", err)
	}
	if err := clientConn.Write(ctx, websocket.MessageBinary, clientData); err != nil {
		t.Fatalf("send client hello: %v", err)
	}

	_, message, err := clientConn.Read(ctx)
	if err != nil {
		t.Fatalf("read welcome frame: %v", err)
	}
	var frame protocolpb.Frame
	if err := proto.Unmarshal(message, &frame); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	if frame.GetWelcome() == nil {
		t.Fatalf("expected welcome frame, got %+v", frame.Payload)
	}
}

func TestShareRevokeDisconnectsActiveShareClient(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)
	ts := newTestServer(t, server.Handler())
	defer ts.Close()

	access, err := store.CreateAccessToken(user.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hostConn, _, err := websocket.Dial(ctx, wsURL(ts.URL, "/ws/host"), &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + access.Token}},
	})
	if err != nil {
		t.Fatalf("host dial: %v", err)
	}
	defer func() {
		_ = hostConn.Close(websocket.StatusNormalClosure, "bye")
	}()
	hostHello := &protocolpb.Frame{
		SessionId: "session_revoke_share",
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			Cols:         80,
			Rows:         24,
			WantsControl: true,
			ClientType:   "host",
		}},
	}
	hostData, err := proto.Marshal(hostHello)
	if err != nil {
		t.Fatalf("marshal host hello: %v", err)
	}
	if err := hostConn.Write(ctx, websocket.MessageBinary, hostData); err != nil {
		t.Fatalf("send host hello: %v", err)
	}

	share, err := store.CreateShareToken("session_revoke_share", ShareScopeView, time.Hour, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateShareToken: %v", err)
	}
	clientConn, _, err := websocket.Dial(ctx, wsURL(ts.URL, "/ws/client?token="+url.QueryEscape(share.Token)), nil)
	if err != nil {
		t.Fatalf("client dial with share query token: %v", err)
	}
	defer func() {
		_ = clientConn.Close(websocket.StatusNormalClosure, "bye")
	}()
	clientHello := &protocolpb.Frame{
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			ClientId:     "revoked-share",
			WantsControl: false,
			ClientType:   "cli",
		}},
	}
	clientData, err := proto.Marshal(clientHello)
	if err != nil {
		t.Fatalf("marshal client hello: %v", err)
	}
	if err := clientConn.Write(ctx, websocket.MessageBinary, clientData); err != nil {
		t.Fatalf("send client hello: %v", err)
	}
	if _, _, err := clientConn.Read(ctx); err != nil {
		t.Fatalf("read welcome frame: %v", err)
	}

	revokeBody, _ := json.Marshal(shareRevokeRequest{Token: share.Token})
	revokeCtx, cancelRevoke := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelRevoke()
	req, err := http.NewRequestWithContext(revokeCtx, http.MethodPost, ts.URL+"/share/revoke", bytes.NewReader(revokeBody))
	if err != nil {
		t.Fatalf("new revoke request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+access.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("share revoke: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("share revoke status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if got := server.Hub.ClientCount("session_revoke_share"); got != 0 {
		t.Fatalf("active clients after revoke = %d, want 0", got)
	}
}

func TestWSReadEOFSuppressed(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)

	var buf lockedBuffer
	logger := pslog.NewWithOptions(context.Background(), &buf, pslog.Options{
		Mode:             pslog.ModeStructured,
		DisableTimestamp: true,
		NoColor:          true,
	})
	server := NewHTTPServer(store, users, auth, logger, nil)
	ts := newTestServer(t, server.Handler())
	defer ts.Close()

	access, err := store.CreateAccessToken(user.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hostConn, _, err := websocket.Dial(ctx, wsURL(ts.URL, "/ws/host"), &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + access.Token}},
	})
	if err != nil {
		t.Fatalf("host dial: %v", err)
	}
	defer func() {
		_ = hostConn.Close(websocket.StatusNormalClosure, "bye")
	}()
	hostHello := &protocolpb.Frame{
		SessionId: "session_eof",
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			Cols:         80,
			Rows:         24,
			WantsControl: true,
			ClientType:   "host",
		}},
	}
	hostData, err := proto.Marshal(hostHello)
	if err != nil {
		t.Fatalf("marshal host hello: %v", err)
	}
	if err := hostConn.Write(ctx, websocket.MessageBinary, hostData); err != nil {
		t.Fatalf("send host hello: %v", err)
	}

	clientConn, _, err := websocket.Dial(ctx, wsURL(ts.URL, "/ws/client"), &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + access.Token}},
	})
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	frame := &protocolpb.Frame{
		SessionId: "session_eof",
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			ClientId:     "client_eof",
			WantsControl: true,
			ClientType:   "client",
		}},
	}
	data, err := proto.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal client hello: %v", err)
	}
	if err := clientConn.Write(ctx, websocket.MessageBinary, data); err != nil {
		t.Fatalf("send client hello: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := clientConn.CloseNow(); err != nil {
		t.Fatalf("close now: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	logs := buf.String()
	if strings.Contains(logs, "\"msg\":\"relay.ws.read.failed\"") {
		t.Fatalf("unexpected read failed log: %s", logs)
	}
}

func TestHostSessionClosedFrameMarksSessionInactiveImmediately(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)
	ts := newTestServer(t, server.Handler())
	defer ts.Close()

	access, err := store.CreateAccessToken(user.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hostConn, _, err := websocket.Dial(ctx, wsURL(ts.URL, "/ws/host"), &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + access.Token}},
	})
	if err != nil {
		t.Fatalf("host dial: %v", err)
	}
	defer func() {
		_ = hostConn.Close(websocket.StatusNormalClosure, "bye")
	}()

	const sessionID = "session_closed_frame"
	hostHello := &protocolpb.Frame{
		SessionId: sessionID,
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			Cols:         80,
			Rows:         24,
			WantsControl: true,
			ClientType:   "host",
		}},
	}
	hostData, err := proto.Marshal(hostHello)
	if err != nil {
		t.Fatalf("marshal host hello: %v", err)
	}
	if err := hostConn.Write(ctx, websocket.MessageBinary, hostData); err != nil {
		t.Fatalf("send host hello: %v", err)
	}
	waitForActiveSessionCount(t, ts.URL, access.Token, 1, 2*time.Second)

	closedFrame := &protocolpb.Frame{
		SessionId: sessionID,
		Payload: &protocolpb.Frame_SessionClosed{SessionClosed: &protocolpb.SessionClosed{
			Reason: "terminated",
		}},
	}
	closedData, err := proto.Marshal(closedFrame)
	if err != nil {
		t.Fatalf("marshal session closed frame: %v", err)
	}
	if err := hostConn.Write(ctx, websocket.MessageBinary, closedData); err != nil {
		t.Fatalf("send session closed frame: %v", err)
	}

	waitForActiveSessionCount(t, ts.URL, access.Token, 0, 2*time.Second)
	if server.Hub.HasHost(sessionID) {
		t.Fatalf("expected host to be unregistered after session closed frame")
	}
}

func TestHostWebsocketCloseStillMarksSessionInactive(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)
	server := NewHTTPServer(store, users, auth, nil, nil)
	dataDir := t.TempDir()
	server.DataDir = dataDir
	ts := newTestServer(t, server.Handler())
	defer ts.Close()

	access, err := store.CreateAccessToken(user.Username, DefaultAccessTokenTTL, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hostConn, _, err := websocket.Dial(ctx, wsURL(ts.URL, "/ws/host"), &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + access.Token}},
	})
	if err != nil {
		t.Fatalf("host dial: %v", err)
	}

	const sessionID = "session_close_fallback"
	hostHello := &protocolpb.Frame{
		SessionId: sessionID,
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			Cols:         80,
			Rows:         24,
			WantsControl: true,
			ClientType:   "host",
		}},
	}
	hostData, err := proto.Marshal(hostHello)
	if err != nil {
		t.Fatalf("marshal host hello: %v", err)
	}
	if err := hostConn.Write(ctx, websocket.MessageBinary, hostData); err != nil {
		t.Fatalf("send host hello: %v", err)
	}
	waitForPersistedSessionStatus(t, dataDir, sessionID, "active", 2*time.Second)
	waitForActiveSessionCount(t, ts.URL, access.Token, 1, 2*time.Second)

	_ = hostConn.Close(websocket.StatusNormalClosure, "done")

	waitForPersistedSessionStatus(t, dataDir, sessionID, "inactive", 2*time.Second)
	waitForActiveSessionCount(t, ts.URL, access.Token, 0, 2*time.Second)
	if server.Hub.HasHost(sessionID) {
		t.Fatalf("expected host to be unregistered after websocket close")
	}
}

func waitForPersistedSessionStatus(t *testing.T, dir, sessionID, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastStatus string
	var lastErr error
	for time.Now().Before(deadline) {
		loaded, err := LoadStore(dir)
		if err == nil {
			session, ok := loaded.GetSession(sessionID)
			if ok {
				lastStatus = session.Status
				if session.Status == want {
					return
				}
			}
		} else {
			lastErr = err
		}
		time.Sleep(25 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("persisted session status for %s: last load error: %v", sessionID, lastErr)
	}
	t.Fatalf("persisted session status for %s = %q, want %q", sessionID, lastStatus, want)
}

func waitForActiveSessionCount(t *testing.T, baseURL, token string, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		sessions, err := listActiveSessions(t, baseURL, token)
		if err == nil && len(sessions) == want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	sessions, err := listActiveSessions(t, baseURL, token)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	t.Fatalf("active session count=%d, want %d", len(sessions), want)
}

func listActiveSessions(t *testing.T, baseURL, token string) ([]Session, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/sessions", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sessions status: %s", resp.Status)
	}
	var sessions []Session
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, err
	}
	return sessions, nil
}

func newTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	return httptest.NewServer(handler)
}

func wsURL(base, path string) string {
	ws := strings.Replace(base, "http://", "ws://", 1)
	ws = strings.Replace(ws, "https://", "wss://", 1)
	return ws + path
}
