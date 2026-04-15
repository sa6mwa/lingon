package ptytest

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/protocolpb"
)

// Direction describes frame flow in the proxy.
type Direction string

const (
	// DirClientToServer indicates a frame traveling from client to server.
	DirClientToServer Direction = "client->server"
	// DirServerToClient indicates a frame traveling from server to client.
	DirServerToClient Direction = "server->client"
)

// FrameRecord captures a proxied websocket frame.
type FrameRecord struct {
	Time      time.Time
	Role      string
	Direction Direction
	SessionID string
	Seq       uint64
	Payload   string
	Raw       []byte
}

// WSRecorder stores websocket frame records for tests.
type WSRecorder struct {
	mu     sync.Mutex
	frames []FrameRecord
}

// NewWSRecorder constructs a recorder for websocket frames.
func NewWSRecorder() *WSRecorder {
	return &WSRecorder{}
}

func (r *WSRecorder) record(role string, dir Direction, data []byte) {
	if r == nil {
		return
	}
	rec := FrameRecord{
		Time:      time.Now(),
		Role:      role,
		Direction: dir,
		Raw:       append([]byte(nil), data...),
		Payload:   "unknown",
	}
	var frame protocolpb.Frame
	if err := proto.Unmarshal(data, &frame); err == nil {
		rec.SessionID = frame.SessionId
		rec.Seq = frame.Seq
		switch frame.Payload.(type) {
		case *protocolpb.Frame_Snapshot:
			rec.Payload = "snapshot"
		case *protocolpb.Frame_Diff:
			rec.Payload = "diff"
		case *protocolpb.Frame_Hello:
			rec.Payload = "hello"
		case *protocolpb.Frame_Welcome:
			rec.Payload = "welcome"
		case *protocolpb.Frame_In:
			rec.Payload = "input"
		case *protocolpb.Frame_Command:
			rec.Payload = "command"
		case *protocolpb.Frame_Resize:
			rec.Payload = "resize"
		case *protocolpb.Frame_Control:
			rec.Payload = "control"
		case *protocolpb.Frame_Activity:
			rec.Payload = "activity"
		case *protocolpb.Frame_Error:
			rec.Payload = "error"
		case *protocolpb.Frame_WallInactivityStatus:
			rec.Payload = "wall_inactivity_status"
		default:
			rec.Payload = "other"
		}
	}
	r.mu.Lock()
	r.frames = append(r.frames, rec)
	r.mu.Unlock()
}

// Frames returns a snapshot of all recorded frames.
func (r *WSRecorder) Frames() []FrameRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]FrameRecord, len(r.frames))
	copy(out, r.frames)
	return out
}

// Count returns the number of frames that match the filter.
func (r *WSRecorder) Count(role, sessionID string, dir Direction) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, rec := range r.frames {
		if role != "" && rec.Role != role {
			continue
		}
		if sessionID != "" && rec.SessionID != sessionID {
			continue
		}
		if dir != "" && rec.Direction != dir {
			continue
		}
		count++
	}
	return count
}

// WaitCount waits until Count reaches min or timeout elapses.
func (r *WSRecorder) WaitCount(role, sessionID string, dir Direction, min int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if r.Count(role, sessionID, dir) >= min {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return r.Count(role, sessionID, dir) >= min
}

type wsProxy struct {
	upstreamHTTP *url.URL
	upstreamWS   string
	basePath     string
	recorder     *WSRecorder
}

func newWSProxy(upstream, basePath string, recorder *WSRecorder) (http.Handler, error) {
	upstreamURL, err := url.Parse(upstream)
	if err != nil {
		return nil, err
	}
	wsBase := *upstreamURL
	wsBase.Scheme = "wss"
	return &wsProxy{
		upstreamHTTP: upstreamURL,
		upstreamWS:   strings.TrimRight(wsBase.String(), "/"),
		basePath:     basePath,
		recorder:     recorder,
	}, nil
}

func (p *wsProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isWSPath(p.basePath, r.URL.Path) {
		p.proxyWS(w, r)
		return
	}
	rp := httputil.NewSingleHostReverseProxy(p.upstreamHTTP)
	rp.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	rp.ServeHTTP(w, r)
}

func isWSPath(basePath, path string) bool {
	return strings.HasSuffix(path, basePath+"/ws/client") || strings.HasSuffix(path, basePath+"/ws/host")
}

func (p *wsProxy) proxyWS(w http.ResponseWriter, r *http.Request) {
	role := "client"
	if strings.HasSuffix(r.URL.Path, "/ws/host") {
		role = "host"
	}
	clientConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: false,
	})
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	upstreamURL := p.upstreamWS + r.URL.Path
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}
	headers := http.Header{}
	if auth := r.Header.Get("Authorization"); auth != "" {
		headers.Set("Authorization", auth)
	}
	if cookie := r.Header.Get("Cookie"); cookie != "" {
		headers.Set("Cookie", cookie)
	}
	upConn, _, err := websocket.Dial(ctx, upstreamURL, &websocket.DialOptions{
		HTTPClient: &http.Client{
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		},
		HTTPHeader: headers,
	})
	if err != nil {
		_ = clientConn.Close(websocket.StatusInternalError, "upstream dial failed")
		return
	}

	defer func() {
		_ = clientConn.Close(websocket.StatusNormalClosure, "closing")
		_ = upConn.Close(websocket.StatusNormalClosure, "closing")
	}()

	errCh := make(chan error, 2)
	go func() {
		errCh <- proxyLoop(ctx, clientConn, upConn, p.recorder, role, DirClientToServer)
	}()
	go func() {
		errCh <- proxyLoop(ctx, upConn, clientConn, p.recorder, role, DirServerToClient)
	}()

	<-errCh
	cancel()
}

func proxyLoop(ctx context.Context, src, dst *websocket.Conn, recorder *WSRecorder, role string, dir Direction) error {
	for {
		msgType, data, err := src.Read(ctx)
		if err != nil {
			return err
		}
		if recorder != nil {
			recorder.record(role, dir, data)
		}
		if err := dst.Write(ctx, msgType, data); err != nil {
			return err
		}
	}
}
