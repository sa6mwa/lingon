package attach

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"pkt.systems/lingon/internal/host"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/server"
	"pkt.systems/lingon/internal/tlsmgr"
)

type sizeProvider struct {
	mu   sync.RWMutex
	cols int
	rows int
}

type byteCollector struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *byteCollector) Add(data []byte) {
	c.mu.Lock()
	_, _ = c.buf.Write(data)
	c.mu.Unlock()
}

func (c *byteCollector) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

type frameCollector struct {
	mu    sync.Mutex
	count int
	last  *protocolpb.Frame
}

func (c *frameCollector) Add(frame *protocolpb.Frame) {
	c.mu.Lock()
	c.count++
	c.last = frame
	c.mu.Unlock()
}

func (c *frameCollector) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

func (c *frameCollector) Last() *protocolpb.Frame {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

type boolFlag struct {
	mu sync.Mutex
	ok bool
}

func (b *boolFlag) Set() {
	b.mu.Lock()
	b.ok = true
	b.mu.Unlock()
}

func (b *boolFlag) Get() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ok
}

func (s *sizeProvider) Size() (int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cols, s.rows
}

func (s *sizeProvider) Set(cols, rows int) {
	s.mu.Lock()
	s.cols, s.rows = cols, rows
	s.mu.Unlock()
}

func TestEndToEndHostAttachFlow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tlsDir := filepath.Join(home, ".lingon", "tls")
	if err := tlsmgr.GenerateAll(context.Background(), tlsDir, "", nil); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	cert, err := tlsmgr.LoadLocalServerCert(tlsDir)
	if err != nil {
		t.Fatalf("LoadLocalServerCert: %v", err)
	}

	usersPath := filepath.Join(home, ".lingon", "users.json")
	users := relay.NewUserStore()
	if _, err := relay.CreateUser(users, "test", "pass", time.Now().UTC()); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := users.Save(usersPath); err != nil {
		t.Fatalf("Save users: %v", err)
	}

	store := relay.NewStore()
	access, err := store.CreateAccessToken("test", time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	auth := relay.NewAuthenticator(users)
	hub := relay.NewHub(nil)
	relayServer := relay.NewHTTPServer(store, users, auth, nil, hub)
	relayServer.UsersFile = usersPath
	relayServer.DataDir = filepath.Join(home, ".lingon")

	handler := server.WrapBasePath("/v1", relayServer.Handler())
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	endpoint := srv.URL + "/v1"

	hostCtx, hostCancel := context.WithCancel(context.Background())
	t.Cleanup(hostCancel)
	hostErr := make(chan error, 1)
	hostInput := &byteCollector{}
	ptyRead := &byteCollector{}
	hostFrames := &frameCollector{}
	hostFrameHasTWO := &boolFlag{}
	h := &host.Host{
		Endpoint:  endpoint,
		Token:     access.Token,
		SessionID: "session_test",
		Cols:      80,
		Rows:      24,
		Command:   []string{"/bin/cat"},
		OnInput: func(data []byte) {
			hostInput.Add(data)
		},
		OnPTYRead: func(data []byte) {
			ptyRead.Add(data)
		},
		OnFrame: func(frame *protocolpb.Frame) {
			hostFrames.Add(frame)
			if frameContains(frame, "TWO") {
				hostFrameHasTWO.Set()
			}
		},
	}
	go func() {
		hostErr <- h.Run(hostCtx)
	}()

	waitUntil(t, 5*time.Second, func() bool {
		return hub.HasHost("session_test")
	}, hostErr)

	in1, w1 := io.Pipe()
	defer w1.Close()
	out1 := &bytes.Buffer{}
	size1 := &sizeProvider{cols: 80, rows: 24}
	c1 := &Client{
		Endpoint:       endpoint,
		SessionID:      "session_test",
		AccessToken:    access.Token,
		RequestControl: true,
		ClientID:       "client1",
		Stdin:          in1,
		Stdout:         out1,
		Stderr:         io.Discard,
		TermSize:       size1.Size,
	}
	ctx1, cancel1 := context.WithCancel(context.Background())
	t.Cleanup(cancel1)
	c1Err := make(chan error, 1)
	go func() {
		c1Err <- c1.Run(ctx1)
	}()

	waitUntil(t, 5*time.Second, func() bool {
		c1.mu.RLock()
		defer c1.mu.RUnlock()
		return c1.holderID == "client1"
	}, hostErr, c1Err)

	in2, w2 := io.Pipe()
	defer w2.Close()
	out2 := &bytes.Buffer{}
	size2 := &sizeProvider{cols: 80, rows: 24}
	c2 := &Client{
		Endpoint:       endpoint,
		SessionID:      "session_test",
		AccessToken:    access.Token,
		RequestControl: false,
		ClientID:       "client2",
		Stdin:          in2,
		Stdout:         out2,
		Stderr:         io.Discard,
		TermSize:       size2.Size,
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	t.Cleanup(cancel2)
	c2Err := make(chan error, 1)
	go func() {
		c2Err <- c2.Run(ctx2)
	}()

	waitUntil(t, 5*time.Second, func() bool {
		c2.mu.RLock()
		defer c2.mu.RUnlock()
		return c2.holderID != ""
	}, hostErr, c1Err, c2Err)

	framesBefore := hostFrames.Count()
	_, _ = w2.Write([]byte("TWO\r\n"))

	waitUntil(t, 5*time.Second, func() bool {
		c1.mu.RLock()
		defer c1.mu.RUnlock()
		return c1.holderID == "client2"
	}, hostErr, c1Err, c2Err)

	waitUntil(t, 5*time.Second, func() bool {
		return hostFrames.Count() > framesBefore
	}, hostErr, c1Err, c2Err)

	waitUntil(t, 5*time.Second, func() bool {
		return strings.Contains(hostInput.String(), "TWO")
	}, hostErr, c1Err, c2Err)

	waitUntil(t, 5*time.Second, func() bool {
		return strings.Contains(ptyRead.String(), "TWO")
	}, hostErr, c1Err, c2Err)

	waitUntil(t, 5*time.Second, func() bool {
		return hostFrameHasTWO.Get()
	}, hostErr, c1Err, c2Err)

	waitUntilDebug(t, 5*time.Second, func() bool {
		snap := c1.getSnapshot()
		return snapshotContains(snap, "TWO")
	}, func() string {
		return snapshotPreview(c1.getSnapshot(), 6, 40) + "\n" + frameSummary(hostFrames.Last())
	}, hostErr, c1Err, c2Err)

	size2.Set(100, 30)
	_ = syscall.Kill(os.Getpid(), syscall.SIGWINCH)

	waitUntil(t, 5*time.Second, func() bool {
		snap := c1.getSnapshot()
		return snap != nil && snap.Cols == 100 && snap.Rows == 30
	}, hostErr, c1Err, c2Err)
}

func snapshotContains(snap *protocolpb.Snapshot, want string) bool {
	if snap == nil {
		return false
	}
	cols := int(snap.Cols)
	rows := int(snap.Rows)
	if cols <= 0 || rows <= 0 {
		return false
	}
	var sb strings.Builder
	sb.Grow(cols * rows)
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			idx := y*cols + x
			if idx >= len(snap.Runes) {
				sb.WriteRune(' ')
				continue
			}
			sb.WriteRune(rune(snap.Runes[idx]))
		}
		sb.WriteRune('\n')
	}
	return strings.Contains(sb.String(), want)
}

func waitUntil(t *testing.T, timeout time.Duration, fn func() bool, errs ...<-chan error) {
	waitUntilDebug(t, timeout, fn, nil, errs...)
}

func waitUntilDebug(t *testing.T, timeout time.Duration, fn func() bool, debug func() string, errs ...<-chan error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, ch := range errs {
			select {
			case err := <-ch:
				if err == nil {
					t.Fatalf("unexpected early exit")
				}
				t.Fatalf("unexpected error: %v", err)
			default:
			}
		}
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	if debug != nil {
		t.Fatalf("condition not met before timeout: %s", debug())
	}
	t.Fatalf("condition not met before timeout")
}

func snapshotPreview(snap *protocolpb.Snapshot, maxRows, maxCols int) string {
	if snap == nil {
		return "snapshot=<nil>"
	}
	cols := int(snap.Cols)
	rows := int(snap.Rows)
	if cols <= 0 || rows <= 0 {
		return "snapshot=empty"
	}
	if maxCols <= 0 || maxCols > cols {
		maxCols = cols
	}
	if maxRows <= 0 || maxRows > rows {
		maxRows = rows
	}
	var sb strings.Builder
	sb.WriteString("snapshot:\n")
	for y := 0; y < maxRows; y++ {
		for x := 0; x < maxCols; x++ {
			idx := y*cols + x
			if idx >= len(snap.Runes) {
				sb.WriteRune(' ')
				continue
			}
			r := rune(snap.Runes[idx])
			if r == 0 {
				r = ' '
			}
			sb.WriteRune(r)
		}
		if y < maxRows-1 {
			sb.WriteRune('\n')
		}
	}
	return sb.String()
}

func frameSummary(frame *protocolpb.Frame) string {
	if frame == nil {
		return "last frame=<nil>"
	}
	switch payload := frame.Payload.(type) {
	case *protocolpb.Frame_Snapshot:
		if payload.Snapshot == nil {
			return "last frame=snapshot<nil>"
		}
		return "last frame=snapshot"
	case *protocolpb.Frame_Diff:
		if payload.Diff == nil {
			return "last frame=diff<nil>"
		}
		return "last frame=diff"
	default:
		return "last frame=other"
	}
}

func frameContains(frame *protocolpb.Frame, want string) bool {
	if frame == nil {
		return false
	}
	if snap := frame.GetSnapshot(); snap != nil {
		return snapshotContains(snap, want)
	}
	if diff := frame.GetDiff(); diff != nil {
		return diffContains(diff, want)
	}
	return false
}

func diffContains(diff *protocolpb.Diff, want string) bool {
	if diff == nil {
		return false
	}
	for _, row := range diff.DiffRows {
		var sb strings.Builder
		for _, r := range row.Runes {
			if r == 0 {
				sb.WriteRune(' ')
				continue
			}
			sb.WriteRune(rune(r))
		}
		if strings.Contains(sb.String(), want) {
			return true
		}
	}
	return false
}
