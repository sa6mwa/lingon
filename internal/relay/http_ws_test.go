package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/pslog"
)

func TestHostConnectLogsReconnected(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)

	var buf bytes.Buffer
	logger := pslog.NewWithOptions(&buf, pslog.Options{
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

func TestWSHostRejectsDuplicateSessionID(t *testing.T) {
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

	readCtx, readCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer readCancel()
	_, payload, err := hostB.Read(readCtx)
	if err != nil {
		t.Fatalf("read duplicate-host rejection: %v", err)
	}
	var frame protocolpb.Frame
	if err := proto.Unmarshal(payload, &frame); err != nil {
		t.Fatalf("decode rejection frame: %v", err)
	}
	if frame.GetError() == nil {
		t.Fatalf("expected error frame for duplicate host, got %+v", frame.Payload)
	}
	if got := frame.GetError().GetMessage(); got != errSessionHasActiveHost.Error() {
		t.Fatalf("duplicate-host message=%q, want %q", got, errSessionHasActiveHost.Error())
	}
	if !frame.GetError().GetSessionRejected() {
		t.Fatalf("expected session_rejected marker on duplicate-host rejection")
	}

	if !server.Hub.HasHost("session-dup") {
		t.Fatalf("expected original host to remain active")
	}
}

func TestClientConnectLogsReconnected(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)

	var buf bytes.Buffer
	logger := pslog.NewWithOptions(&buf, pslog.Options{
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
		HTTPHeader: map[string][]string{"Cookie": {shareCookie}},
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
}

func TestWSReadEOFSuppressed(t *testing.T) {
	store := NewStore()
	users := NewUserStore()
	user, err := SeedTestUser(users)
	if err != nil {
		t.Fatalf("SeedTestUser: %v", err)
	}
	auth := NewAuthenticator(users)

	var buf bytes.Buffer
	logger := pslog.NewWithOptions(&buf, pslog.Options{
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
	waitForActiveSessionCount(t, ts.URL, access.Token, 1, 2*time.Second)

	_ = hostConn.Close(websocket.StatusNormalClosure, "done")

	waitForActiveSessionCount(t, ts.URL, access.Token, 0, 2*time.Second)
	if server.Hub.HasHost(sessionID) {
		t.Fatalf("expected host to be unregistered after websocket close")
	}
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
